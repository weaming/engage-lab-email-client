package push

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	host            = "https://email.api.engagelab.cc"
	regularSendApi  = host + "/v1/mail/send"
	templateSendApi = host + "/v1/mail/sendtemplate"
	templateListApi = host + "/v1/templates"

	// export ENGAGE_LAB_EMAIL_API_KEY=user_name:abcdxxxxxxxxxxxxxxxxxxxxxxxxxefg
	apiKeyEnv = "ENGAGE_LAB_EMAIL_API_KEY"
)

type EmailRequest[T EmailBody] struct {
	From string   `json:"from"` // required
	To   []string `json:"to"`   // required. Up to 100 addresses are supported.
	Body *T       `json:"body"` // required

	// Optional fields customized by the customer. The maximum size is 1KB.
	CustomArgs map[string]any `json:"custom_args,omitempty"`

	// ID of this sending request; 128 characters maximum.
	RequestId string `json:"request_id,omitempty"`
}

type EmailResponse struct {
	HTTPStatus int    `json:"http_status"`
	RequestId  string `json:"request_id,omitempty"`

	// Non address list sending (send_mode=0 or send_mode=1)
	EmailIds []string `json:"email_ids,omitempty"`
	// Address list sending (send_mode=2) Response-success
	TaskID []int `json:"task_id,omitempty"`

	// when error happens
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (r *EmailResponse) Json() string {
	if r == nil {
		return ""
	}
	bs, err := json.Marshal(r)
	if err != nil {
		log.Printf("failed to marshal EmailResponse: %v", err)
		return ""
	}
	return string(bs)
}

type EmailBody interface {
	RegularEmail | TemplateEmail
}

// https://www.engagelab.com/docs/email/rest-api/deliverlies#regular-delivery
type BodyCommon struct {
	Cc      []string `json:"cc,omitempty"`
	Bcc     []string `json:"bcc,omitempty"`
	ReplyTo []string `json:"reply_to,omitempty"`

	// For variable replacement of mail content. Up to 1MB；When send_mode=0 or send_mode=1, this parameter is valid.
	// Value must be array, size(value) must be equal to size(to)
	Vars map[string][]any `json:"vars,omitempty"`

	// For replacing variables in dynamic templates. The maximum size supported is 1MB;
	// this parameter is valid when send_mode = 0 or send_mode = 1.
	DynamicVars map[string]any `json:"dynamic_vars,omitempty"`

	LabelID   int    `json:"label_id,omitempty"`   // Label ID used for this sending
	LabelName string `json:"label_name,omitempty"` // Label name used for this sending

	// Headers is used to customize the header field of the message.
	// The format is json object, and the format is' {"User Define": "123", "User Custom": "abc"} '.
	// However, the key string cannot contain the following values (case insensitive):
	// DKIM-Signature, Received, Sender, Date, From, To, Reply-To, Cc, Bcc, Subject, Content-Type,
	// Content-Transfer-Encoding, X-SENDCLOUD-UUID, X-SENDCLOUD-LOG, X-Remote-Web-IP,
	// X-SMTPAPI, Return-Path,X-SENDCLOUD-LOG-NEW
	Headers     map[string]string `json:"headers,omitempty"`
	Attachments []*Attachment     `json:"attachments,omitempty"`
	Settings    *Settings         `json:"settings,omitempty"`
}

type RegularEmail struct {
	*BodyCommon

	Subject string        `json:"subject"` // required. Support variables, emoji.
	Content *EmailContent `json:"content"` // required
}

type TemplateEmail struct {
	*BodyCommon

	Subject            string `json:"subject"`              // NOT required. Support variables, emoji.
	TemplateInvokeName string `json:"template_invoke_name"` // required. Only include letters, numbers and underscores.
}

type EmailContent struct {
	HTML        string `json:"html,omitempty"` // HTML or Text is required
	Text        string `json:"text,omitempty"`
	PreviewText string `json:"preview_text,omitempty"`
}

type Attachment struct {
	Content     string `json:"content,omitempty"`
	Filename    string `json:"filename,omitempty"`
	Disposition string `json:"disposition,omitempty"`
	ContentID   string `json:"content_id,omitempty"`
}

type Settings struct {
	// Sending method. The default is 0
	// 0 means sending separately;
	// 1 means broadcast sending, and all recipients will be displayed at the same time;
	// 2 indicates that the address list is sent. The value of to is the address list's address.
	SendMode int `json:"send_mode"`

	// Whether to return email ID, default is true.
	ReturnEmailID bool `json:"return_email_id"`

	// Whether to use sandbox mode, the default is false.
	// If it is true, the mail will not be delivered, and only the request parameters will be verified for validity.
	Sandbox bool `json:"sandbox"`

	Notification        bool  `json:"notification"`
	OpenTracking        bool  `json:"open_tracking"`
	ClickTracking       bool  `json:"click_tracking"`
	UnsubscribeTracking bool  `json:"unsubscribe_tracking"`
	UnsubscribePageID   []int `json:"unsubscribe_page_id"`
}

type EngageLabEmailClient struct {
	apiKey string
	client *http.Client
}

func NewEngageLabEmailClient(apiKey string) *EngageLabEmailClient {
	if apiKey == "" {
		apiKey = os.Getenv(apiKeyEnv)
		if apiKey == "" {
			log.Panicf("Environment variable %s is not set", apiKeyEnv)
		}
	}
	log.Printf("EngageLab Email API Key: %s", Secret(apiKey))
	return &EngageLabEmailClient{
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *EngageLabEmailClient) SendRegular(
	from string,
	to []string,
	subject string,
	text, html string,
	reqId string,
) (*EmailResponse, error) {
	return c.Send(nil, from, to, subject, html, text, "", "", reqId)
}

func (c *EngageLabEmailClient) SendTemplate(
	from string,
	to []string,
	subject string,
	template string,
	vars map[string][]any,
	reqId string,
) (*EmailResponse, error) {
	bodyCommon := BodyCommon{Vars: vars}
	return c.Send(&bodyCommon, from, to, subject, "", "", "", template, reqId)
}

func (c *EngageLabEmailClient) getHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(c.apiKey)),
		"Content-Type":  "application/json;charset=utf-8",
	}
}

// Consider using SendRegular or SendTemplate instead.
func (c *EngageLabEmailClient) Send(
	bodyCommon *BodyCommon,
	from string,
	to []string,
	subject string,
	text, html, preview string,
	template string,
	reqId string,
) (*EmailResponse, error) {
	headers := c.getHeaders()
	var (
		api      string
		reqBytes []byte
		err      error
	)
	if reqId == "" {
		reqId = uuid.New().String()
	}
	if template != "" {
		api = templateSendApi
		request := EmailRequest[TemplateEmail]{
			From: from,
			To:   to,
			Body: &TemplateEmail{
				BodyCommon:         bodyCommon,
				Subject:            subject,
				TemplateInvokeName: template,
			},
			RequestId: reqId,
		}
		reqBytes, err = json.Marshal(request)
	} else {
		if subject == "" {
			return nil, errors.New("subject is required")
		}
		if html == "" && text == "" {
			return nil, errors.New("html or text is required")
		}

		api = regularSendApi
		request := EmailRequest[RegularEmail]{
			From: from,
			To:   to,
			Body: &RegularEmail{
				BodyCommon: bodyCommon,
				Subject:    subject,
				Content: &EmailContent{
					HTML:        html,
					Text:        text,
					PreviewText: preview,
				},
			},
			RequestId: reqId,
		}
		reqBytes, err = json.Marshal(request)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", api, bytes.NewBuffer(reqBytes))
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		return nil, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	log.Printf("POST to %s with headers: %v, body: %s", api, headers, string(reqBytes))

	rsp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}

	response := &EmailResponse{HTTPStatus: rsp.StatusCode}
	if rsp.Body == nil {
		return response, fmt.Errorf("response body is nil")
	}

	resBytes, err := io.ReadAll(rsp.Body)
	if err != nil {
		return response, fmt.Errorf("failed to read response body: %v", err)
	}

	err = json.Unmarshal(resBytes, &response)
	if err != nil {
		return response, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	log.Printf("email resp: %+v", response)
	return response, nil
}

// https://www.engagelab.com/docs/email/rest-api/email-template#query-batch
type TemplatesResponse struct {
	HTTPStatus int         `json:"http_status"`
	Result     []*Template `json:"result"`
	Total      int         `json:"total"`
	Count      int         `json:"count"`
}

type Template struct {
	TemplateID         int    `json:"template_id"`
	TemplateInvokeName string `json:"template_invoke_name"`
	Name               string `json:"name"`
	Subject            string `json:"subject"`

	HTML        string `json:"html,omitempty"`
	Text        string `json:"text,omitempty"`
	PreviewText string `json:"preview_text"`
	CreateTime  string `json:"create_time"`
	UpdateTime  string `json:"update_time"`

	// when error happens
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (c *EngageLabEmailClient) GetTemplates() ([]*Template, error) {
	headers := c.getHeaders()

	xs := []*Template{}
	page := 1
	limit := 100
	for {
		offset := limit * (page - 1)

		api := fmt.Sprintf("%s?offset=%d&limit=%d", templateListApi, offset, limit)
		req, err := http.NewRequest(http.MethodGet, api, nil)
		if err != nil {
			log.Printf("Failed to create request: %v", err)
			return nil, err
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		log.Printf("GET %s with headers: %v", api, headers)

		rsp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %v", err)
		}

		response := &TemplatesResponse{HTTPStatus: rsp.StatusCode}
		if rsp.Body == nil {
			return nil, fmt.Errorf("response body is nil")
		}

		resBytes, err := io.ReadAll(rsp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %v", err)
		}

		err = json.Unmarshal(resBytes, &response)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %v", err)
		}
		log.Printf("email templates response: %+v", response)

		xs = append(xs, response.Result...)
		if len(xs) == response.Total || len(response.Result) < int(limit) {
			break
		}
		page++
		time.Sleep(time.Millisecond * 500)
	}

	return xs, nil
}

// 隐藏字符串中间的部分
func Secret(key string) string {
	n := len(key)
	if n <= 1 {
		return strings.Repeat("*", n)
	}

	// 计算中间三分之一长度（向上取整）
	m := (n + 2) / 3
	start := (n - m) / 2
	end := start + m

	// 处理边界情况
	if start < 0 {
		start = 0
	}
	if end > n {
		end = n
	}

	return key[:start] + strings.Repeat("*", m) + key[end:]
}
