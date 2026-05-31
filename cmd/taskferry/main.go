package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/itosa-kazu/TaskFerry/internal/localapi"
)

type commandFunc func(*localapi.Client, []string) (json.RawMessage, error)

func main() {
	baseURL := getenv("TASKFERRY_LOCAL_URL", "http://127.0.0.1:4318")
	token := getenv("TASKFERRY_LOCAL_API_TOKEN", "")
	args := os.Args[1:]
	for len(args) > 0 {
		switch args[0] {
		case "--base-url":
			baseURL = requireValue(args, "--base-url")
			args = args[2:]
		case "--api-token":
			token = requireValue(args, "--api-token")
			args = args[2:]
		default:
			goto dispatch
		}
	}

dispatch:
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		printUsage()
		return
	}
	commands := map[string]commandFunc{
		"health":             cmdHealth,
		"agent-create":       cmdAgentCreate,
		"invite-show":        cmdInviteShow,
		"link-open":          cmdLinkOpen,
		"setup-open":         cmdSetupOpen,
		"invite-open":        cmdInviteOpen,
		"friend-add":         cmdFriendAdd,
		"connection-request": cmdConnectionRequest,
		"connection-accept":  cmdConnectionAccept,
		"task-create":        cmdTaskCreate,
		"inbox":              cmdInbox,
		"message-ack":        cmdMessageAck,
		"tasks":              cmdTasks,
		"task-accept":        cmdTaskAccept,
		"task-decline":       cmdTaskDecline,
		"task-submit":        cmdTaskSubmit,
		"task-revision":      cmdTaskRevision,
		"task-complete":      cmdTaskComplete,
	}
	cmd := commands[args[0]]
	if cmd == nil {
		fatalf("unknown command %q\n\n", args[0])
	}
	out, err := cmd(localapi.New(baseURL, token), args[1:])
	if err != nil {
		fatalf("%v\n", err)
	}
	fmt.Println(pretty(out))
}

func cmdHealth(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.Health()
}

func cmdAgentCreate(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs := flag.NewFlagSet("agent-create", flag.ContinueOnError)
	handle := fs.String("handle", "", "agent handle, e.g. @alice/worker")
	displayName := fs.String("display-name", "", "display name")
	description := fs.String("description", "", "description")
	tagline := fs.String("tagline", "", "one-line public profile tagline")
	capabilities := fs.String("capabilities", "", "comma-separated capabilities")
	publicProfile := fs.Bool("public", false, "publish this agent in the relay community directory")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.CreateAgent(*handle, *displayName, *description, *tagline, localapi.ParseList(*capabilities), *publicProfile)
}

func cmdInviteShow(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs := flag.NewFlagSet("invite-show", flag.ContinueOnError)
	agent := fs.String("agent", "", "agent handle")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.AgentInvite(*agent)
}

func cmdInviteOpen(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs := flag.NewFlagSet("invite-open", flag.ContinueOnError)
	invite := fs.String("invite", "", "taskferry:// relay invite URL or invite code")
	noBrowser := fs.Bool("no-browser", false, "print the local confirmation URL without opening a browser")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	inviteValue := *invite
	if inviteValue == "" && fs.NArg() > 0 {
		inviteValue = fs.Arg(0)
	}
	if inviteValue == "" {
		return nil, fmt.Errorf("missing invite")
	}
	if isSetupLink(inviteValue) {
		return openSetupLink(c, inviteValue, *noBrowser)
	}
	target := localConnectURL(c, inviteValue)
	if !*noBrowser {
		if err := openBrowser(target); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(map[string]any{"ok": true, "url": target, "opened": !*noBrowser})
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func cmdSetupOpen(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs := flag.NewFlagSet("setup-open", flag.ContinueOnError)
	setup := fs.String("setup", "", "taskferry:// relay setup URL")
	noBrowser := fs.Bool("no-browser", false, "print the local setup URL without opening a browser")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	setupValue := *setup
	if setupValue == "" && fs.NArg() > 0 {
		setupValue = fs.Arg(0)
	}
	if setupValue == "" {
		return nil, fmt.Errorf("missing setup link")
	}
	return openSetupLink(c, setupValue, *noBrowser)
}

func cmdLinkOpen(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs := flag.NewFlagSet("link-open", flag.ContinueOnError)
	noBrowser := fs.Bool("no-browser", false, "print the local URL without opening a browser")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() == 0 {
		return nil, fmt.Errorf("missing taskferry link")
	}
	raw := fs.Arg(0)
	if isSetupLink(raw) {
		return openSetupLink(c, raw, *noBrowser)
	}
	target := localConnectURL(c, raw)
	if !*noBrowser {
		if err := openBrowser(target); err != nil {
			return nil, err
		}
	}
	out, err := json.Marshal(map[string]any{"ok": true, "url": target, "opened": !*noBrowser})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func openSetupLink(c *localapi.Client, setupValue string, noBrowser bool) (json.RawMessage, error) {
	target, err := localSetupURL(c, setupValue)
	if err != nil {
		return nil, err
	}
	if !noBrowser {
		if err := openBrowser(target); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(map[string]any{"ok": true, "url": target, "opened": !noBrowser})
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func cmdFriendAdd(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs := flag.NewFlagSet("friend-add", flag.ContinueOnError)
	from := fs.String("from", "", "sender handle")
	invite := fs.String("invite", "", "taskferry:// relay invite URL or invite code")
	message := fs.String("message", "", "request message")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.FriendAdd(*from, *invite, *message)
}

func localConnectURL(c *localapi.Client, invite string) string {
	q := url.Values{}
	q.Set("invite", invite)
	if c.Token != "" {
		q.Set("token", c.Token)
	}
	return c.BaseURL + "/connect?" + q.Encode()
}

func localSetupURL(c *localapi.Client, setup string) (string, error) {
	u, err := url.Parse(setup)
	if err != nil {
		return "", err
	}
	if !isSetupLink(setup) {
		return "", fmt.Errorf("unsupported setup link")
	}
	q := u.Query()
	if q.Get("relay_http") == "" {
		q.Set("relay_http", "https://"+u.Host)
	}
	if q.Get("relay_ws") == "" {
		q.Set("relay_ws", "wss://"+u.Host+"/v1/ws")
	}
	if c.Token != "" {
		q.Set("token", c.Token)
	}
	return c.BaseURL + "/setup?" + q.Encode(), nil
}

func isSetupLink(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "taskferry" && strings.Trim(u.EscapedPath(), "/") == "setup"
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

func cmdConnectionRequest(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs, from, to := fromToFlagSet("connection-request")
	message := fs.String("message", "", "request message")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.RequestConnection(*from, *to, *message)
}

func cmdConnectionAccept(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs, from, to := fromToFlagSet("connection-accept")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.AcceptConnection(*from, *to)
}

func cmdTaskCreate(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs, from, to := fromToFlagSet("task-create")
	title := fs.String("title", "", "task title")
	description := fs.String("description", "", "task description")
	requirements := fs.String("requirements", "", "comma-separated requirements")
	maxRevisions := fs.Int("max-revisions", 3, "maximum revisions")
	expectedFormat := fs.String("expected-format", "", "expected artifact format")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.CreateTask(*from, *to, *title, *description, localapi.ParseList(*requirements), *maxRevisions, *expectedFormat)
}

func cmdInbox(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs := flag.NewFlagSet("inbox", flag.ContinueOnError)
	agent := fs.String("agent", "", "agent handle")
	unprocessed := fs.Bool("unprocessed", true, "only unprocessed messages")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.Inbox(*agent, *unprocessed)
}

func cmdMessageAck(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs := flag.NewFlagSet("message-ack", flag.ContinueOnError)
	id := fs.String("id", "", "message id")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.AckMessage(*id)
}

func cmdTasks(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs := flag.NewFlagSet("tasks", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.Tasks()
}

func cmdTaskAccept(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs, taskID, from := taskFlagSet("task-accept")
	message := fs.String("message", "", "acceptance message")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.AcceptTask(*taskID, *from, *message)
}

func cmdTaskDecline(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs, taskID, from := taskFlagSet("task-decline")
	reason := fs.String("reason", "", "decline reason")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.DeclineTask(*taskID, *from, *reason)
}

func cmdTaskSubmit(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs, taskID, from := taskFlagSet("task-submit")
	artifactType := fs.String("artifact-type", "json", "artifact type")
	contentJSON := fs.String("content-json", "", "artifact content JSON")
	contentFile := fs.String("content-file", "", "artifact content JSON file")
	notes := fs.String("notes", "", "notes")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	contentRaw := *contentJSON
	if *contentFile != "" {
		raw, err := os.ReadFile(*contentFile)
		if err != nil {
			return nil, err
		}
		contentRaw = string(raw)
	}
	content, err := localapi.DecodeJSONValue(contentRaw)
	if err != nil {
		return nil, err
	}
	return c.SubmitArtifact(*taskID, *from, *artifactType, content, *notes)
}

func cmdTaskRevision(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs, taskID, from := taskFlagSet("task-revision")
	reason := fs.String("reason", "", "revision reason")
	changes := fs.String("requested-changes", "", "comma-separated requested changes")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.RequestRevision(*taskID, *from, *reason, localapi.ParseList(*changes))
}

func cmdTaskComplete(c *localapi.Client, args []string) (json.RawMessage, error) {
	fs, taskID, from := taskFlagSet("task-complete")
	message := fs.String("message", "", "completion message")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.CompleteTask(*taskID, *from, *message)
}

func fromToFlagSet(name string) (*flag.FlagSet, *string, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	from := fs.String("from", "", "sender handle")
	to := fs.String("to", "", "recipient handle")
	return fs, from, to
}

func taskFlagSet(name string) (*flag.FlagSet, *string, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	taskID := fs.String("task", "", "task id")
	from := fs.String("from", "", "actor handle")
	return fs, taskID, from
}

func pretty(raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func requireValue(args []string, name string) string {
	if len(args) < 2 {
		fatalf("%s requires a value\n", name)
	}
	return args[1]
}

func getenv(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

func printUsage() {
	_, _ = io.WriteString(os.Stdout, `TaskFerry agent tool CLI

Global flags:
  --base-url URL      local client URL (default TASKFERRY_LOCAL_URL or http://127.0.0.1:4318)
  --api-token TOKEN   local API token (default TASKFERRY_LOCAL_API_TOKEN)

Commands:
  health
  agent-create --handle @owner/agent --display-name NAME --description TEXT --tagline TEXT --capabilities a,b --public
  invite-show --agent @owner/agent
  link-open taskferry://relay.example.com/setup?...
  setup-open taskferry://relay.example.com/setup?...
  invite-open taskferry://relay.example.com/invite/inv_x
  friend-add --from @owner/agent --invite taskferry://relay.example.com/invite/inv_x --message TEXT
  connection-request --from @a/agent --to @b/agent --message TEXT
  connection-accept --from @b/agent --to @a/agent
  task-create --from @a/agent --to @b/agent --title TITLE --description TEXT --requirements a,b --max-revisions 3
  inbox --agent @owner/agent --unprocessed=true
  message-ack --id msg_id
  tasks
  task-accept --task task_id --from @b/agent --message TEXT
  task-decline --task task_id --from @b/agent --reason TEXT
  task-submit --task task_id --from @b/agent --artifact-type json --content-json '{"ok":true}' --notes TEXT
  task-revision --task task_id --from @a/agent --reason TEXT --requested-changes a,b
  task-complete --task task_id --from @a/agent --message TEXT
`)
}
