package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	defaultBaseURL  = "http://127.0.0.1:9000"
	defaultDomain   = "inbox.example.com"
	serverVersion   = "0.1.0"
	protocolVersion = "2024-11-05"
)

var (
	baseURL string
	domain  string
	user    string
	pass    string
	client  = &http.Client{Timeout: 30 * time.Second}

	hrefRe    = regexp.MustCompile(`(?i)href\s*=\s*["']([^"']+)["']`)
	srcRe     = regexp.MustCompile(`(?i)src\s*=\s*["']([^"']+)["']`)
	urlRe     = regexp.MustCompile(`(https?://[^\s<>"')\]}]+)`)
	codeRe    = regexp.MustCompile(`\b\d{4,8}\b`)
	mailboxRe = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)
	messageRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type messageMeta struct {
	Mailbox string   `json:"mailbox"`
	ID      string   `json:"id"`
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Date    string   `json:"date"`
	Size    int64    `json:"size"`
	Seen    bool     `json:"seen"`
}

type message struct {
	Mailbox string              `json:"mailbox"`
	ID      string              `json:"id"`
	From    string              `json:"from"`
	To      []string            `json:"to"`
	Subject string              `json:"subject"`
	Date    string              `json:"date"`
	Size    int64               `json:"size"`
	Header  map[string][]string `json:"header"`
	Body    struct {
		Text string `json:"text"`
		HTML string `json:"html"`
	} `json:"body"`
}

type newAddressArgs struct {
	Prefix    string `json:"prefix,omitempty"`
	LocalPart string `json:"local_part,omitempty"`
}

type listArgs struct {
	Mailbox         string `json:"mailbox"`
	Limit           int    `json:"limit,omitempty"`
	SubjectContains string `json:"subject_contains,omitempty"`
	FromContains    string `json:"from_contains,omitempty"`
}

type getArgs struct {
	Mailbox string `json:"mailbox"`
	ID      string `json:"id"`
}

type waitArgs struct {
	Mailbox         string `json:"mailbox"`
	SubjectContains string `json:"subject_contains,omitempty"`
	FromContains    string `json:"from_contains,omitempty"`
	TimeoutS        int    `json:"timeout_s,omitempty"`
	PollIntervalS   int    `json:"poll_interval_s,omitempty"`
}

type mailboxArgs struct {
	Mailbox string `json:"mailbox"`
}

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Handler     func(json.RawMessage) (any, error)
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func loadConfig() error {
	baseURL = strings.TrimRight(envOr("INBOX_BASE_URL", defaultBaseURL), "/")
	domain = envOr("INBOX_DOMAIN", defaultDomain)
	user = os.Getenv("INBOX_BASIC_USER")
	pass = os.Getenv("INBOX_BASIC_PASS")

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("INBOX_BASE_URL must be an absolute HTTP or HTTPS URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("INBOX_BASE_URL must use http or https")
	}
	if strings.ContainsAny(domain, "@/ \t\r\n") {
		return fmt.Errorf("INBOX_DOMAIN is not a valid email domain")
	}
	if (user == "") != (pass == "") {
		return fmt.Errorf("INBOX_BASIC_USER and INBOX_BASIC_PASS must be set together")
	}
	return nil
}

func validateMailbox(mailbox string) error {
	if !mailboxRe.MatchString(mailbox) {
		return errors.New("mailbox must contain only letters, numbers, dots, underscores, plus signs, and hyphens")
	}
	return nil
}

func validateMessageID(id string) error {
	if !messageRe.MatchString(id) {
		return errors.New("message id contains unsupported characters")
	}
	return nil
}

func api(method, path string) ([]byte, error) {
	req, err := http.NewRequest(method, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	req.Header.Set("User-Agent", "agent-test-inbox-mcp/"+serverVersion)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	return body, nil
}

func listMessages(mailbox string) ([]messageMeta, error) {
	if err := validateMailbox(mailbox); err != nil {
		return nil, err
	}
	body, err := api(http.MethodGet, "/api/v1/mailbox/"+url.PathEscape(mailbox))
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			return []messageMeta{}, nil
		}
		return nil, err
	}
	var messages []messageMeta
	if err := json.Unmarshal(body, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func getMessage(mailbox, id string) (*message, error) {
	if err := validateMailbox(mailbox); err != nil {
		return nil, err
	}
	if err := validateMessageID(id); err != nil {
		return nil, err
	}
	body, err := api(http.MethodGet, "/api/v1/mailbox/"+url.PathEscape(mailbox)+"/"+url.PathEscape(id))
	if err != nil {
		return nil, err
	}
	var result message
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func clearMailbox(mailbox string) error {
	if err := validateMailbox(mailbox); err != nil {
		return err
	}
	_, err := api(http.MethodDelete, "/api/v1/mailbox/"+url.PathEscape(mailbox))
	if err != nil && strings.Contains(err.Error(), "HTTP 404") {
		return nil
	}
	return err
}

func deleteMessage(mailbox, id string) error {
	if err := validateMailbox(mailbox); err != nil {
		return err
	}
	if err := validateMessageID(id); err != nil {
		return err
	}
	_, err := api(http.MethodDelete, "/api/v1/mailbox/"+url.PathEscape(mailbox)+"/"+url.PathEscape(id))
	return err
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n...(truncated)"
}

func randomHex(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func sanitizeLocal(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' {
			result.WriteRune(char)
		} else {
			result.WriteByte('-')
		}
	}
	local := strings.Trim(result.String(), "-")
	if local == "" {
		return "agent"
	}
	return local
}

func extractLinks(html, text string) []string {
	seen := map[string]bool{}
	links := []string{}
	add := func(link string) {
		link = strings.TrimSpace(link)
		if link != "" && !seen[link] {
			seen[link] = true
			links = append(links, link)
		}
	}
	for _, expression := range []*regexp.Regexp{hrefRe, srcRe, urlRe} {
		for _, match := range expression.FindAllStringSubmatch(html, -1) {
			add(match[1])
		}
		for _, match := range expression.FindAllStringSubmatch(text, -1) {
			add(match[1])
		}
	}
	return links
}

func extractCodes(text string) []string {
	seen := map[string]bool{}
	codes := []string{}
	for _, code := range codeRe.FindAllString(text, -1) {
		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	return codes
}

func enrichMessage(item *message) map[string]any {
	text := item.Body.Text
	if text == "" && item.Body.HTML != "" {
		text = item.Body.HTML
	}
	return map[string]any{
		"mailbox": item.Mailbox,
		"id":      item.ID,
		"from":    item.From,
		"to":      item.To,
		"subject": item.Subject,
		"date":    item.Date,
		"size":    item.Size,
		"text":    truncate(text, 20000),
		"html":    truncate(item.Body.HTML, 40000),
		"links":   extractLinks(item.Body.HTML, text),
		"codes":   extractCodes(text),
	}
}

func toolNewAddress(raw json.RawMessage) (any, error) {
	var args newAddressArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("bad arguments: %w", err)
		}
	}
	local := sanitizeLocal(args.LocalPart)
	if args.LocalPart == "" {
		suffix, err := randomHex(8)
		if err != nil {
			return nil, fmt.Errorf("generate address: %w", err)
		}
		local = fmt.Sprintf("%s-%d-%s", sanitizeLocal(args.Prefix), time.Now().Unix(), suffix)
	}
	return map[string]any{"address": local + "@" + domain, "mailbox": local, "domain": domain}, nil
}

func toolListMessages(raw json.RawMessage) (any, error) {
	var args listArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("bad arguments: %w", err)
	}
	if args.Mailbox == "" {
		return nil, errors.New("mailbox is required (use new_address first)")
	}
	messages, err := listMessages(args.Mailbox)
	if err != nil {
		return nil, err
	}
	limit := args.Limit
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	result := []messageMeta{}
	for index := len(messages) - 1; index >= 0; index-- {
		item := messages[index]
		if args.SubjectContains != "" && !strings.Contains(strings.ToLower(item.Subject), strings.ToLower(args.SubjectContains)) {
			continue
		}
		if args.FromContains != "" && !strings.Contains(strings.ToLower(item.From), strings.ToLower(args.FromContains)) {
			continue
		}
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func toolGetMessage(raw json.RawMessage) (any, error) {
	var args getArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("bad arguments: %w", err)
	}
	if args.Mailbox == "" || args.ID == "" {
		return nil, errors.New("mailbox and id are required")
	}
	item, err := getMessage(args.Mailbox, args.ID)
	if err != nil {
		return nil, err
	}
	return enrichMessage(item), nil
}

func toolWaitForEmail(raw json.RawMessage) (any, error) {
	var args waitArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("bad arguments: %w", err)
	}
	if args.Mailbox == "" {
		return nil, errors.New("mailbox is required (use new_address first)")
	}
	timeout := args.TimeoutS
	if timeout <= 0 {
		timeout = 60
	}
	if timeout > 300 {
		timeout = 300
	}
	poll := args.PollIntervalS
	if poll <= 0 {
		poll = 4
	}
	if poll > 30 {
		poll = 30
	}
	matches := func(item messageMeta) bool {
		return (args.SubjectContains == "" || strings.Contains(strings.ToLower(item.Subject), strings.ToLower(args.SubjectContains))) &&
			(args.FromContains == "" || strings.Contains(strings.ToLower(item.From), strings.ToLower(args.FromContains)))
	}
	seen := map[string]bool{}
	check := func(messages []messageMeta) (*message, error) {
		for index := len(messages) - 1; index >= 0; index-- {
			item := messages[index]
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			if matches(item) {
				return getMessage(args.Mailbox, item.ID)
			}
		}
		return nil, nil
	}
	if messages, err := listMessages(args.Mailbox); err == nil {
		if item, err := check(messages); err == nil && item != nil {
			return enrichMessage(item), nil
		}
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(poll) * time.Second)
		messages, err := listMessages(args.Mailbox)
		if err != nil {
			continue
		}
		item, err := check(messages)
		if err == nil && item != nil {
			return enrichMessage(item), nil
		}
	}
	return nil, fmt.Errorf("timed out after %ds waiting for a message in %s (subject=%q from=%q)", timeout, args.Mailbox, args.SubjectContains, args.FromContains)
}

func toolClearInbox(raw json.RawMessage) (any, error) {
	var args mailboxArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("bad arguments: %w", err)
	}
	if args.Mailbox == "" {
		return nil, errors.New("mailbox is required")
	}
	if err := clearMailbox(args.Mailbox); err != nil {
		return nil, err
	}
	return map[string]any{"cleared": true, "mailbox": args.Mailbox}, nil
}

func toolDeleteMessage(raw json.RawMessage) (any, error) {
	var args getArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("bad arguments: %w", err)
	}
	if args.Mailbox == "" || args.ID == "" {
		return nil, errors.New("mailbox and id are required")
	}
	if err := deleteMessage(args.Mailbox, args.ID); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": true, "id": args.ID}, nil
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

var tools = []toolDef{
	{
		Name:        "new_address",
		Description: "Generate a fresh throwaway email address. Use one address per test run. Returns the full address and mailbox local part.",
		InputSchema: objectSchema(map[string]any{
			"prefix":     stringProperty("Optional memorable prefix. Defaults to agent."),
			"local_part": stringProperty("Optional explicit local part."),
		}),
		Handler: toolNewAddress,
	},
	{
		Name:        "list_messages",
		Description: "List messages in a mailbox, newest first, optionally filtered by subject or sender.",
		InputSchema: objectSchema(map[string]any{
			"mailbox":          stringProperty("Mailbox local part returned by new_address."),
			"limit":            map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "description": "Maximum messages. Defaults to 10."},
			"subject_contains": stringProperty("Case-insensitive subject filter."),
			"from_contains":    stringProperty("Case-insensitive sender filter."),
		}, "mailbox"),
		Handler: toolListMessages,
	},
	{
		Name:        "get_message",
		Description: "Fetch a message with text and HTML bodies, extracted links, and numeric confirmation codes.",
		InputSchema: objectSchema(map[string]any{
			"mailbox": stringProperty("Mailbox local part returned by new_address."),
			"id":      stringProperty("Message id returned by list_messages."),
		}, "mailbox", "id"),
		Handler: toolGetMessage,
	},
	{
		Name:        "wait_for_email",
		Description: "Poll a mailbox for an email, optionally matching subject or sender, and return the fully parsed message.",
		InputSchema: objectSchema(map[string]any{
			"mailbox":          stringProperty("Mailbox local part returned by new_address."),
			"subject_contains": stringProperty("Case-insensitive subject filter."),
			"from_contains":    stringProperty("Case-insensitive sender filter."),
			"timeout_s":        map[string]any{"type": "integer", "minimum": 1, "maximum": 300, "description": "Timeout in seconds. Defaults to 60."},
			"poll_interval_s":  map[string]any{"type": "integer", "minimum": 1, "maximum": 30, "description": "Polling interval in seconds. Defaults to 4."},
		}, "mailbox"),
		Handler: toolWaitForEmail,
	},
	{
		Name:        "clear_inbox",
		Description: "Delete every message in a mailbox.",
		InputSchema: objectSchema(map[string]any{"mailbox": stringProperty("Mailbox local part to empty.")}, "mailbox"),
		Handler:     toolClearInbox,
	},
	{
		Name:        "delete_message",
		Description: "Delete one message from a mailbox.",
		InputSchema: objectSchema(map[string]any{
			"mailbox": stringProperty("Mailbox local part."),
			"id":      stringProperty("Message id to delete."),
		}, "mailbox", "id"),
		Handler: toolDeleteMessage,
	},
}

func handle(request *rpcRequest) rpcResponse {
	response := rpcResponse{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "agent-test-inbox-mcp", "version": serverVersion},
		}
	case "ping":
		response.Result = map[string]any{}
	case "tools/list":
		list := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			list = append(list, map[string]any{"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema})
		}
		response.Result = map[string]any{"tools": list}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
			return response
		}
		for _, tool := range tools {
			if tool.Name != params.Name {
				continue
			}
			result, err := tool.Handler(params.Arguments)
			if err != nil {
				response.Result = toolResult{Content: []toolContent{{Type: "text", Text: err.Error()}}, IsError: true}
				return response
			}
			body, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				body = []byte(fmt.Sprintf("%v", result))
			}
			response.Result = toolResult{Content: []toolContent{{Type: "text", Text: string(body)}}}
			return response
		}
		response.Error = &rpcError{Code: -32602, Message: "unknown tool: " + params.Name}
	default:
		response.Error = &rpcError{Code: -32601, Message: "method not found: " + request.Method}
	}
	return response
}

func run(input io.Reader, output, errorOutput io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1<<20), 4<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var request rpcRequest
		if err := json.Unmarshal(line, &request); err != nil {
			fmt.Fprintf(errorOutput, "agent-test-inbox-mcp: bad input: %v\n", err)
			continue
		}
		if len(request.ID) == 0 {
			continue
		}
		body, err := json.Marshal(handle(&request))
		if err != nil {
			fmt.Fprintf(errorOutput, "agent-test-inbox-mcp: marshal: %v\n", err)
			continue
		}
		if _, err := fmt.Fprintln(output, string(body)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func main() {
	if err := loadConfig(); err != nil {
		fmt.Fprintln(os.Stderr, "agent-test-inbox-mcp:", err)
		os.Exit(1)
	}
	if err := run(os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "agent-test-inbox-mcp:", err)
		os.Exit(1)
	}
}
