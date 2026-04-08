package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client sends messages to a Discord webhook.
type Client struct {
	webhookURL string
	http       *http.Client
}

// NewClient creates a Discord webhook client.
func NewClient(webhookURL string) *Client {
	return &Client{
		webhookURL: webhookURL,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

type embedImage struct {
	URL string `json:"url,omitempty"`
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type embedFooter struct {
	Text string `json:"text"`
}

type embedAuthor struct {
	Name string `json:"name"`
}

type embed struct {
	Author      *embedAuthor `json:"author,omitempty"`
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color"`
	Thumbnail   *embedImage  `json:"thumbnail,omitempty"`
	Fields      []embedField `json:"fields,omitempty"`
	Footer      *embedFooter `json:"footer,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

type webhookPayload struct {
	Embeds []embed `json:"embeds"`
}

// Embed is a builder for constructing rich Discord embeds.
type Embed struct {
	data embed
}

// NewEmbed creates a new embed builder.
func NewEmbed() *Embed {
	return &Embed{}
}

func (e *Embed) Author(name string) *Embed {
	e.data.Author = &embedAuthor{Name: name}
	return e
}

func (e *Embed) Title(title string) *Embed {
	e.data.Title = title
	return e
}

func (e *Embed) Description(desc string) *Embed {
	e.data.Description = desc
	return e
}

func (e *Embed) Color(color int) *Embed {
	e.data.Color = color
	return e
}

func (e *Embed) Thumbnail(url string) *Embed {
	if url != "" {
		e.data.Thumbnail = &embedImage{URL: url}
	}
	return e
}

func (e *Embed) Field(name, value string, inline bool) *Embed {
	if value != "" {
		e.data.Fields = append(e.data.Fields, embedField{Name: name, Value: value, Inline: inline})
	}
	return e
}

func (e *Embed) Footer(text string) *Embed {
	e.data.Footer = &embedFooter{Text: text}
	return e
}

func (e *Embed) Timestamp(t time.Time) *Embed {
	e.data.Timestamp = t.UTC().Format(time.RFC3339)
	return e
}

// Send posts the embed to the given client's webhook.
func (c *Client) Send(embeds ...*Embed) error {
	var data []embed
	for _, e := range embeds {
		data = append(data, e.data)
	}
	payload := webhookPayload{Embeds: data}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling discord payload: %w", err)
	}

	resp, err := c.http.Post(c.webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sending discord webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// TestConnection sends a test embed to verify the webhook is working.
func TestConnection(webhookURL string) (bool, string, error) {
	client := NewClient(webhookURL)
	e := NewEmbed().
		Author("MediaGate").
		Description("Webhook connection test successful!").
		Color(0x7289DA).
		Timestamp(time.Now())
	if err := client.Send(e); err != nil {
		return false, fmt.Sprintf("Connection failed: %v", err), nil
	}
	return true, "Test message sent to Discord", nil
}
