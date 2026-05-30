package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

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
	capabilities := fs.String("capabilities", "", "comma-separated capabilities")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c.CreateAgent(*handle, *displayName, *description, localapi.ParseList(*capabilities))
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
  agent-create --handle @owner/agent --display-name NAME --description TEXT --capabilities a,b
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
