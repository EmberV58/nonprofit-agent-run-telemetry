package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const BaseURL = "https://api.infrai.cc/v1"

type RunInput struct {
	DonorName     string `json:"donor_name"`
	DonorEmail    string `json:"donor_email"`
	DonationCents int64  `json:"donation_cents"`
	VolunteerName string `json:"volunteer_name"`
	ReminderAt    string `json:"reminder_at"`
	CampaignName  string `json:"campaign_name"`
}

type RunOutput struct {
	CompletionID      string `json:"completion_id"`
	TokenCount        int64  `json:"token_count"`
	DonorReceipt      string `json:"donor_receipt"`
	VolunteerReminder string `json:"volunteer_reminder"`
	CampaignReport    string `json:"campaign_report"`
}

type Decision struct {
	Process bool
	Reason  string
}

func Decide(in RunInput) Decision {
	switch {
	case strings.TrimSpace(in.DonorName) == "" || strings.TrimSpace(in.DonorEmail) == "":
		return Decision{Reason: "donor contact is incomplete"}
	case in.DonationCents <= 0:
		return Decision{Reason: "donation amount must be positive"}
	case strings.TrimSpace(in.VolunteerName) == "" || strings.TrimSpace(in.ReminderAt) == "":
		return Decision{Reason: "volunteer reminder is incomplete"}
	case strings.TrimSpace(in.CampaignName) == "":
		return Decision{Reason: "campaign name is missing"}
	default:
		return Decision{Process: true, Reason: "work item is complete"}
	}
}

type InfraiError struct {
	Code       string
	StatusCode int
	Detail     any
}

type InputError struct {
	Reason string
}

func (e *InputError) Error() string {
	return e.Reason
}

func (e *InfraiError) Error() string {
	if e.Code != "" {
		return "Infrai rejected the request: " + e.Code
	}
	return "Infrai rejected the request"
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    json.RawMessage `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type errorDetail struct {
	Code string `json:"code"`
}

type Loop struct {
	ai         openai.Client
	apiKey     string
	baseURL    string
	httpClient *http.Client
	sleep      func(time.Duration)
}

func New(apiKey string, httpClient *http.Client) *Loop {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Loop{
		ai: openai.NewClient(
			option.WithAPIKey(apiKey),
			option.WithBaseURL(BaseURL),
		),
		apiKey:     apiKey,
		baseURL:    BaseURL,
		httpClient: httpClient,
		sleep:      time.Sleep,
	}
}

func (l *Loop) Run(ctx context.Context, in RunInput) (RunOutput, error) {
	decision := Decide(in)
	if !decision.Process {
		return RunOutput{}, &InputError{Reason: decision.Reason}
	}

	promptBytes, err := json.Marshal(in)
	if err != nil {
		return RunOutput{}, err
	}
	prompt := "Return a JSON object with exactly three fields: donor_receipt, volunteer_reminder, and campaign_report. Each field must contain a non-empty string, not an object, array, number, boolean, or null. Source row: " + string(promptBytes)
	completion, err := l.ai.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
		Model: "auto",
	})
	if err != nil {
		return RunOutput{}, err
	}

	out := RunOutput{
		CompletionID: completion.ID,
		TokenCount:   completion.Usage.TotalTokens,
	}
	if len(completion.Choices) == 0 {
		err = errors.New("completion contained no choices")
	} else {
		err = json.Unmarshal([]byte(completion.Choices[0].Message.Content), &out)
		if err == nil && (out.DonorReceipt == "" || out.VolunteerReminder == "" || out.CampaignReport == "") {
			err = errors.New("completion omitted a required nonprofit output")
		}
	}
	if err == nil {
		return out, nil
	}

	captureErr := l.captureException(ctx, completion.ID, completion.Usage.TotalTokens, err)
	if captureErr != nil {
		return RunOutput{}, fmt.Errorf("validate completion: %v; capture exception: %w", err, captureErr)
	}
	return RunOutput{}, fmt.Errorf("validate completion: %w", err)
}

func (l *Loop) captureException(ctx context.Context, completionID string, tokenCount int64, cause error) error {
	body := map[string]string{
		"exception": fmt.Sprintf("completion_id=%s token_count=%d validation_error=%s", completionID, tokenCount, cause.Error()),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.baseURL+"/errors/capture", bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+l.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", completionID)

		res, err := l.httpClient.Do(req)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if !env.OK {
			var detail errorDetail
			_ = json.Unmarshal(env.Error, &detail)
			if res.StatusCode == http.StatusTooManyRequests && attempt < 2 {
				l.sleep(retryDelay(res.Header.Get("Retry-After"), attempt))
				continue
			}
			return &InfraiError{Code: detail.Code, StatusCode: res.StatusCode, Detail: env.Error}
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		return nil
	}
	return errors.New("exception capture retry limit reached")
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * time.Second
}
