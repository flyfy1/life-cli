package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/flyfy1/life-cli/internal/integlife"
	"golang.org/x/term"
)

var Version = "dev"

// Mood represents the user's emotional state when logging.
type Mood string

const (
	MoodHappy      Mood = "happy"
	MoodSad        Mood = "sad"
	MoodAnxious    Mood = "anxious"
	MoodCalm       Mood = "calm"
	MoodExcited    Mood = "excited"
	MoodTired      Mood = "tired"
	MoodFocused    Mood = "focused"
	MoodFrustrated Mood = "frustrated"
)

var validMoods = []Mood{
	MoodHappy, MoodSad, MoodAnxious, MoodCalm,
	MoodExcited, MoodTired, MoodFocused, MoodFrustrated,
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg := integlife.LoadConfig()

	store, err := integlife.OpenStore(cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	client := integlife.NewClient(cfg.APIURL, cfg.APIToken, 10*time.Second)
	service := integlife.NewService(store, client)

	if len(args) == 0 {
		printUsage(os.Stderr)
		return errors.New("missing command or content")
	}

	first := args[0]

	switch first {
	case "login":
		return cmdLogin(ctx, client)

	case "logout":
		return cmdLogout()

	case "sync":
		return cmdSync(ctx, service)

	case "todo":
		return cmdTodo(ctx, service, args[1:])

	case "list":
		return cmdTodoList(ctx, service, args[1:])

	case "ai":
		return cmdAI(ctx, service, args[1:])

	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil

	case "version", "-v", "--version":
		fmt.Printf("life version %s\n", Version)
		return nil
	}

	// Positional logging: life 'content' OR life <category> 'content'
	// Peek at whether first arg looks like a category (no --mood prefix, not a flag).
	logType := "thought"
	var content string
	remaining := args

	// Strip --mood flag from anywhere in remaining args.
	mood, remaining, err := extractMood(remaining)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		printUsage(os.Stderr)
		return errors.New("missing content")
	}

	if len(remaining) == 1 {
		// life 'content'  →  type=thought
		content = strings.TrimSpace(remaining[0])
	} else if len(remaining) == 2 {
		// life <category> 'content'
		logType = strings.TrimSpace(remaining[0])
		content = strings.TrimSpace(remaining[1])
	} else {
		printUsage(os.Stderr)
		return errors.New("too many arguments")
	}

	if content == "" {
		return errors.New("content must be non-empty")
	}
	if logType == "" {
		return errors.New("category must be non-empty")
	}

	fullContent := content
	if mood != "" {
		fullContent = fmt.Sprintf("[mood:%s] %s", mood, content)
	}

	record, synced, syncDetail, err := service.LogAndMaybeSync(ctx, logType, fullContent)
	if err != nil {
		return err
	}

	fmt.Printf("saved: uuid=%s type=%s\n", record.UUID, record.LogType)
	if synced {
		fmt.Printf("sync ok: %s\n", syncDetail)
	} else if syncDetail != "" {
		fmt.Printf("sync skipped: %s\n", syncDetail)
	}
	return nil
}

func cmdTodo(ctx context.Context, service *integlife.Service, args []string) error {
	if len(args) == 0 {
		printTodoUsage(os.Stderr)
		return errors.New("missing todo command")
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("life todo add", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		notes := fs.String("notes", "", "todo notes")
		listRef := fs.String("list", "", "list uuid, prefix, or name")
		order := fs.Float64("order", 0, "sort order")
		jsonOut := fs.Bool("json", false, "print json")
		positionals, err := parseCommandFlags(fs, args[1:], []string{"notes", "list", "order"}, []string{"json"})
		if err != nil {
			return err
		}
		content := strings.TrimSpace(strings.Join(positionals, " "))
		result, err := service.AddTodo(ctx, content, *notes, *listRef, *order)
		if err != nil {
			return err
		}
		printTodoResult(result, *jsonOut)
		return nil
	case "list":
		fs := flag.NewFlagSet("life todo list", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		all := fs.Bool("all", false, "include deleted todos")
		done := fs.Bool("done", false, "show completed todos")
		open := fs.Bool("open", false, "show open todos")
		listRef := fs.String("list", "", "list uuid, prefix, or name")
		jsonOut := fs.Bool("json", false, "print json")
		if _, err := parseCommandFlags(fs, args[1:], []string{"list"}, []string{"all", "done", "open", "json"}); err != nil {
			return err
		}
		if *done && *open {
			return errors.New("--done and --open cannot be used together")
		}
		var completedFilter *bool
		if *done || *open {
			value := *done
			completedFilter = &value
		}
		todos, err := service.ListTodos(*all, completedFilter, *listRef)
		if err != nil {
			return err
		}
		printTodoList(todos, *jsonOut)
		return nil
	case "show":
		fs := flag.NewFlagSet("life todo show", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		jsonOut := fs.Bool("json", false, "print json")
		positionals, err := parseCommandFlags(fs, args[1:], nil, []string{"json"})
		if err != nil {
			return err
		}
		if len(positionals) != 1 {
			return errors.New("usage: life todo show <uuid-or-prefix>")
		}
		todo, err := service.Todo(positionals[0])
		if err != nil {
			return err
		}
		printTodo(todo, *jsonOut)
		return nil
	case "update":
		fs := flag.NewFlagSet("life todo update", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		content := fs.String("content", "", "new content")
		notes := fs.String("notes", "", "new notes")
		listRef := fs.String("list", "", "list uuid, prefix, or name")
		clearList := fs.Bool("clear-list", false, "remove todo from list")
		order := fs.Float64("order", 0, "sort order")
		done := fs.Bool("done", false, "mark completed")
		open := fs.Bool("open", false, "mark open")
		jsonOut := fs.Bool("json", false, "print json")
		positionals, err := parseCommandFlags(fs, args[1:], []string{"content", "notes", "list", "order"}, []string{"clear-list", "done", "open", "json"})
		if err != nil {
			return err
		}
		if len(positionals) != 1 {
			return errors.New("usage: life todo update <uuid-or-prefix> [flags]")
		}
		if *done && *open {
			return errors.New("--done and --open cannot be used together")
		}
		hasChange := flagProvided(fs, "content") || flagProvided(fs, "notes") || flagProvided(fs, "list") ||
			flagProvided(fs, "clear-list") || flagProvided(fs, "order") || flagProvided(fs, "done") || flagProvided(fs, "open")
		if !hasChange {
			return errors.New("todo update requires at least one field flag")
		}
		result, err := service.UpdateTodo(ctx, positionals[0], func(todo *integlife.TodoRecord) error {
			if flagProvided(fs, "content") {
				if strings.TrimSpace(*content) == "" {
					return errors.New("--content must be non-empty")
				}
				todo.Content = strings.TrimSpace(*content)
			}
			if flagProvided(fs, "notes") {
				todo.Notes = *notes
			}
			if flagProvided(fs, "list") {
				list, err := service.TodoList(*listRef)
				if err != nil {
					return err
				}
				todo.ListUUID = list.UUID
			}
			if *clearList {
				todo.ListUUID = ""
			}
			if flagProvided(fs, "order") {
				todo.SortOrder = *order
			}
			if *done || *open {
				now := time.Now().UTC()
				todo.Completed = *done
				if *done {
					todo.CompletedAt = &now
				} else {
					todo.CompletedAt = nil
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		printTodoResult(result, *jsonOut)
		return nil
	case "done":
		fs := flag.NewFlagSet("life todo done", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		undo := fs.Bool("undo", false, "mark open")
		jsonOut := fs.Bool("json", false, "print json")
		positionals, err := parseCommandFlags(fs, args[1:], nil, []string{"undo", "json"})
		if err != nil {
			return err
		}
		if len(positionals) != 1 {
			return errors.New("usage: life todo done <uuid-or-prefix>")
		}
		result, err := service.CompleteTodo(ctx, positionals[0], !*undo)
		if err != nil {
			return err
		}
		printTodoResult(result, *jsonOut)
		return nil
	case "delete":
		fs := flag.NewFlagSet("life todo delete", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		jsonOut := fs.Bool("json", false, "print json")
		positionals, err := parseCommandFlags(fs, args[1:], nil, []string{"json"})
		if err != nil {
			return err
		}
		if len(positionals) != 1 {
			return errors.New("usage: life todo delete <uuid-or-prefix>")
		}
		result, err := service.DeleteTodo(ctx, positionals[0])
		if err != nil {
			return err
		}
		printTodoResult(result, *jsonOut)
		return nil
	case "help", "-h", "--help":
		printTodoUsage(os.Stdout)
		return nil
	default:
		printTodoUsage(os.Stderr)
		return fmt.Errorf("unknown todo command %q", args[0])
	}
}

func cmdTodoList(ctx context.Context, service *integlife.Service, args []string) error {
	if len(args) == 0 {
		printTodoListUsage(os.Stderr)
		return errors.New("missing list command")
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("life list add", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		color := fs.String("color", "", "list color")
		icon := fs.String("icon", "", "list icon")
		order := fs.Int("order", 0, "sort order")
		jsonOut := fs.Bool("json", false, "print json")
		positionals, err := parseCommandFlags(fs, args[1:], []string{"color", "icon", "order"}, []string{"json"})
		if err != nil {
			return err
		}
		name := strings.TrimSpace(strings.Join(positionals, " "))
		result, err := service.AddTodoList(ctx, name, *color, *icon, *order)
		if err != nil {
			return err
		}
		printTodoListResult(result, *jsonOut)
		return nil
	case "list":
		fs := flag.NewFlagSet("life list list", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		all := fs.Bool("all", false, "include deleted lists")
		jsonOut := fs.Bool("json", false, "print json")
		if _, err := parseCommandFlags(fs, args[1:], nil, []string{"all", "json"}); err != nil {
			return err
		}
		lists, err := service.ListTodoLists(*all)
		if err != nil {
			return err
		}
		printTodoLists(lists, *jsonOut)
		return nil
	case "update":
		fs := flag.NewFlagSet("life list update", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		name := fs.String("name", "", "new name")
		color := fs.String("color", "", "new color")
		icon := fs.String("icon", "", "new icon")
		order := fs.Int("order", 0, "sort order")
		jsonOut := fs.Bool("json", false, "print json")
		positionals, err := parseCommandFlags(fs, args[1:], []string{"name", "color", "icon", "order"}, []string{"json"})
		if err != nil {
			return err
		}
		if len(positionals) != 1 {
			return errors.New("usage: life list update <uuid-prefix-or-name> [flags]")
		}
		hasChange := flagProvided(fs, "name") || flagProvided(fs, "color") || flagProvided(fs, "icon") || flagProvided(fs, "order")
		if !hasChange {
			return errors.New("list update requires at least one field flag")
		}
		result, err := service.UpdateTodoList(ctx, positionals[0], func(list *integlife.TodoListRecord) error {
			if flagProvided(fs, "name") {
				if strings.TrimSpace(*name) == "" {
					return errors.New("--name must be non-empty")
				}
				list.Name = strings.TrimSpace(*name)
			}
			if flagProvided(fs, "color") {
				list.Color = strings.TrimSpace(*color)
			}
			if flagProvided(fs, "icon") {
				list.Icon = strings.TrimSpace(*icon)
			}
			if flagProvided(fs, "order") {
				list.SortOrder = *order
			}
			return nil
		})
		if err != nil {
			return err
		}
		printTodoListResult(result, *jsonOut)
		return nil
	case "delete":
		fs := flag.NewFlagSet("life list delete", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		jsonOut := fs.Bool("json", false, "print json")
		positionals, err := parseCommandFlags(fs, args[1:], nil, []string{"json"})
		if err != nil {
			return err
		}
		if len(positionals) != 1 {
			return errors.New("usage: life list delete <uuid-prefix-or-name>")
		}
		result, err := service.DeleteTodoList(ctx, positionals[0])
		if err != nil {
			return err
		}
		printTodoListResult(result, *jsonOut)
		return nil
	case "help", "-h", "--help":
		printTodoListUsage(os.Stdout)
		return nil
	default:
		printTodoListUsage(os.Stderr)
		return fmt.Errorf("unknown list command %q", args[0])
	}
}

func cmdAI(ctx context.Context, service *integlife.Service, args []string) error {
	if len(args) == 0 {
		printAIUsage(os.Stderr)
		return errors.New("missing ai command")
	}
	switch args[0] {
	case "start":
		fs := flag.NewFlagSet("life ai start", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		project := fs.String("project", "", "project ref type:uuid")
		todo := fs.String("todo", "", "todo uuid")
		title := fs.String("title", "", "run title")
		agent := fs.String("agent", "codex", "agent name")
		session := fs.String("session", "", "session id")
		jsonOut := fs.Bool("json", false, "print json")
		newRun := fs.Bool("new", false, "force new run")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := service.StartAITask(ctx, integlife.AIStartOptions{
			Project: *project, TodoUUID: *todo, Title: *title, AgentName: *agent,
			SessionID: *session, SessionExplicit: flagProvided(fs, "session"), NewRun: *newRun,
		})
		if err != nil {
			return err
		}
		printAICommandResult(result, *jsonOut)
		return nil
	case "resume":
		fs := flag.NewFlagSet("life ai resume", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		project := fs.String("project", "", "project ref type:uuid")
		todo := fs.String("todo", "", "todo uuid")
		agent := fs.String("agent", "codex", "agent name")
		session := fs.String("session", "", "session id")
		title := fs.String("title", "", "title for --new")
		jsonOut := fs.Bool("json", false, "print json")
		newRun := fs.Bool("new", false, "create a new run")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, found, err := service.ResumeAITask(ctx, integlife.AIResumeOptions{
			Project: *project, TodoUUID: *todo, AgentName: *agent, SessionID: *session,
			SessionExplicit: flagProvided(fs, "session"), NewRun: *newRun, Title: *title,
		})
		if err != nil {
			return err
		}
		if !found {
			if *jsonOut {
				printJSON(map[string]any{"ok": false, "error": "no resumable run"})
			} else {
				fmt.Println("no resumable AI run found")
			}
			return nil
		}
		printAICommandResult(result, *jsonOut)
		return nil
	case "progress":
		fs := flag.NewFlagSet("life ai progress", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		run := fs.String("run", "", "run uuid")
		phase := fs.String("phase", "", "phase")
		summary := fs.String("summary", "", "summary")
		jsonOut := fs.Bool("json", false, "print json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := service.ProgressAITask(ctx, *run, *phase, *summary)
		if err != nil {
			return err
		}
		printAICommandResult(result, *jsonOut)
		return nil
	case "heartbeat":
		fs := flag.NewFlagSet("life ai heartbeat", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		run := fs.String("run", "", "run uuid")
		summary := fs.String("summary", "", "summary")
		jsonOut := fs.Bool("json", false, "print json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := service.HeartbeatAITask(ctx, *run, *summary)
		if err != nil {
			return err
		}
		printAICommandResult(result, *jsonOut)
		return nil
	case "event":
		fs := flag.NewFlagSet("life ai event", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		run := fs.String("run", "", "run uuid")
		eventType := fs.String("type", "progress", "event type")
		severity := fs.String("severity", "info", "severity")
		title := fs.String("title", "", "title")
		content := fs.String("content", "", "content")
		metadata := fs.String("metadata-json", "{}", "metadata json")
		jsonOut := fs.Bool("json", false, "print json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := service.AddAITaskEvent(ctx, integlife.AIEventOptions{
			RunUUID: *run, EventType: *eventType, Severity: *severity, Title: *title,
			Content: *content, MetadataJSON: *metadata,
		})
		if err != nil {
			return err
		}
		printAICommandResult(result, *jsonOut)
		return nil
	case "block":
		fs := flag.NewFlagSet("life ai block", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		run := fs.String("run", "", "run uuid")
		question := fs.String("question", "", "question")
		jsonOut := fs.Bool("json", false, "print json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := service.BlockAITask(ctx, *run, *question)
		if err != nil {
			return err
		}
		printAICommandResult(result, *jsonOut)
		return nil
	case "complete":
		fs := flag.NewFlagSet("life ai complete", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		run := fs.String("run", "", "run uuid")
		summary := fs.String("summary", "", "summary")
		artifact := fs.String("artifact", "", "artifact path")
		jsonOut := fs.Bool("json", false, "print json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		result, err := service.CompleteAITask(ctx, *run, *summary, *artifact)
		if err != nil {
			return err
		}
		printAICommandResult(result, *jsonOut)
		return nil
	case "status":
		fs := flag.NewFlagSet("life ai status", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		run := fs.String("run", "", "run uuid")
		current := fs.Bool("current", false, "show current active run")
		project := fs.String("project", "", "project ref type:uuid")
		todo := fs.String("todo", "", "todo uuid")
		agent := fs.String("agent", "codex", "agent name")
		session := fs.String("session", "", "session id")
		jsonOut := fs.Bool("json", false, "print json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		var record integlife.AITaskRunRecord
		var found bool
		var err error
		if *current {
			record, found, err = service.CurrentAITask(*project, *todo, *agent, *session)
			if err != nil {
				return err
			}
			if !found {
				if *jsonOut {
					printJSON(map[string]any{"ok": false, "error": "no current run"})
				} else {
					fmt.Println("no current AI run")
				}
				return nil
			}
		} else {
			if strings.TrimSpace(*run) == "" {
				return errors.New("--run is required unless --current is used")
			}
			record, err = service.AITaskStatus(*run)
			if err != nil {
				return err
			}
		}
		printAIRunStatus(record, *jsonOut)
		return nil
	case "resolve":
		fs := flag.NewFlagSet("life ai resolve", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		run := fs.String("run", "", "run uuid")
		preferLocal := fs.Bool("prefer-local", false, "prefer local state")
		newRun := fs.Bool("new-run", false, "create successor run")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*run) == "" {
			return errors.New("--run is required")
		}
		if !*preferLocal && !*newRun {
			return errors.New("resolve is minimal in this release; pass --prefer-local or --new-run to choose an explicit strategy")
		}
		fmt.Printf("resolve pending for run %s: sync conflict handling is minimal in this CLI build; inspect `life ai status --run %s` before retrying sync\n", *run, *run)
		return nil
	case "help", "-h", "--help":
		printAIUsage(os.Stdout)
		return nil
	default:
		printAIUsage(os.Stderr)
		return fmt.Errorf("unknown ai command %q", args[0])
	}
}

// extractMood removes --mood <value> or --mood=<value> from args and returns the mood.
func extractMood(args []string) (Mood, []string, error) {
	out := make([]string, 0, len(args))
	var mood Mood
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if val, ok := strings.CutPrefix(arg, "--mood="); ok {
			if err := validateMood(val); err != nil {
				return "", nil, err
			}
			mood = Mood(val)
		} else if arg == "--mood" {
			if i+1 >= len(args) {
				return "", nil, errors.New("--mood requires a value")
			}
			i++
			if err := validateMood(args[i]); err != nil {
				return "", nil, err
			}
			mood = Mood(args[i])
		} else {
			out = append(out, arg)
		}
	}
	return mood, out, nil
}

func validateMood(val string) error {
	for _, m := range validMoods {
		if string(m) == val {
			return nil
		}
	}
	names := make([]string, len(validMoods))
	for i, m := range validMoods {
		names[i] = string(m)
	}
	return fmt.Errorf("invalid mood %q; choose from: %s", val, strings.Join(names, ", "))
}

func cmdLogin(ctx context.Context, client *integlife.Client) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("username: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)

	fmt.Print("password: ")
	passBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	password := strings.TrimSpace(string(passBytes))

	token, err := client.Login(ctx, username, password)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	if err := integlife.SaveAPIToken(token); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	fmt.Println("login ok: token saved")
	return nil
}

func cmdLogout() error {
	if err := integlife.DeleteAPIToken(); err != nil {
		return fmt.Errorf("logout failed: %w", err)
	}
	fmt.Println("logged out: token removed")
	return nil
}

func cmdSync(ctx context.Context, service *integlife.Service) error {
	result, err := service.SyncPending(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("sync finished: synced=%d pending=%d\n", result.Synced, result.Pending)
	for _, f := range result.Failures {
		fmt.Printf("sync failed: uuid=%s detail=%s\n", f.UUID, f.Detail)
	}
	return nil
}

func printAICommandResult(result integlife.AICommandResult, jsonOut bool) {
	if jsonOut {
		out := map[string]any{
			"ok":          true,
			"run_uuid":    result.Run.UUID,
			"status":      result.Run.Status,
			"synced":      result.Synced,
			"sync_detail": result.SyncDetail,
		}
		if result.Event != nil {
			out["event_uuid"] = result.Event.UUID
			out["event_type"] = result.Event.EventType
		}
		printJSON(out)
		return
	}
	fmt.Printf("ai run: uuid=%s status=%s\n", result.Run.UUID, result.Run.Status)
	if result.Event != nil {
		fmt.Printf("ai event: uuid=%s type=%s\n", result.Event.UUID, result.Event.EventType)
	}
	if result.Synced {
		fmt.Printf("sync ok: %s\n", result.SyncDetail)
	} else if result.SyncDetail != "" {
		fmt.Printf("sync skipped: %s\n", result.SyncDetail)
	}
}

func printAIRunStatus(run integlife.AITaskRunRecord, jsonOut bool) {
	if jsonOut {
		printJSON(map[string]any{
			"ok":                true,
			"run_uuid":          run.UUID,
			"status":            run.Status,
			"title":             run.Title,
			"latest_phase":      run.LatestPhase,
			"latest_summary":    run.LatestSummary,
			"last_sync_error":   run.LastSyncError,
			"client_updated_at": run.ClientUpdatedAt.Format(time.RFC3339Nano),
		})
		return
	}
	fmt.Printf("run: %s\n", run.UUID)
	fmt.Printf("status: %s\n", run.Status)
	if run.Title != "" {
		fmt.Printf("title: %s\n", run.Title)
	}
	if run.LatestPhase != "" {
		fmt.Printf("phase: %s\n", run.LatestPhase)
	}
	if run.LatestSummary != "" {
		fmt.Printf("summary: %s\n", run.LatestSummary)
	}
	if run.LastSyncError != "" {
		fmt.Printf("sync conflict/error: %s\n", run.LastSyncError)
		fmt.Printf("try: life ai resolve --run %s --prefer-local\n", run.UUID)
	}
}

func printTodoResult(result integlife.TodoCommandResult, jsonOut bool) {
	if jsonOut {
		printTodo(result.Todo, true)
		return
	}
	state := "open"
	if result.Todo.Completed {
		state = "done"
	}
	if result.Todo.DeletedAt != nil {
		state = "deleted"
	}
	fmt.Printf("todo: %s [%s] %s\n", result.Todo.UUID, state, result.Todo.Content)
	if result.Synced {
		fmt.Printf("sync ok: %s\n", result.SyncDetail)
	} else if result.SyncDetail != "" {
		fmt.Printf("sync skipped: %s\n", result.SyncDetail)
	}
}

func printTodo(todo integlife.TodoRecord, jsonOut bool) {
	if jsonOut {
		printJSON(todoJSON(todo))
		return
	}
	state := "open"
	if todo.Completed {
		state = "done"
	}
	if todo.DeletedAt != nil {
		state = "deleted"
	}
	fmt.Printf("uuid: %s\n", todo.UUID)
	fmt.Printf("state: %s\n", state)
	fmt.Printf("content: %s\n", todo.Content)
	if todo.Notes != "" {
		fmt.Printf("notes: %s\n", todo.Notes)
	}
	if todo.ListUUID != "" {
		fmt.Printf("list: %s\n", todo.ListUUID)
	}
	if todo.LastSyncError != "" {
		fmt.Printf("sync conflict/error: %s\n", todo.LastSyncError)
	}
}

func printTodoList(todos []integlife.TodoRecord, jsonOut bool) {
	if jsonOut {
		out := make([]map[string]any, 0, len(todos))
		for _, todo := range todos {
			out = append(out, todoJSON(todo))
		}
		printJSON(out)
		return
	}
	if len(todos) == 0 {
		fmt.Println("no todos")
		return
	}
	for _, todo := range todos {
		marker := "[ ]"
		if todo.Completed {
			marker = "[x]"
		}
		if todo.DeletedAt != nil {
			marker = "[-]"
		}
		listSuffix := ""
		if todo.ListUUID != "" {
			listSuffix = " list=" + todo.ListUUID
		}
		syncSuffix := ""
		if todo.LastSyncError != "" {
			syncSuffix = " sync_error=" + todo.LastSyncError
		}
		fmt.Printf("%s %s %s%s%s\n", shortID(todo.UUID), marker, todo.Content, listSuffix, syncSuffix)
	}
}

func printTodoListResult(result integlife.TodoListCommandResult, jsonOut bool) {
	if jsonOut {
		printJSON(todoListJSON(result.List))
		return
	}
	state := "active"
	if result.List.DeletedAt != nil {
		state = "deleted"
	}
	fmt.Printf("list: %s [%s] %s\n", result.List.UUID, state, result.List.Name)
	if result.Synced {
		fmt.Printf("sync ok: %s\n", result.SyncDetail)
	} else if result.SyncDetail != "" {
		fmt.Printf("sync skipped: %s\n", result.SyncDetail)
	}
}

func printTodoLists(lists []integlife.TodoListRecord, jsonOut bool) {
	if jsonOut {
		out := make([]map[string]any, 0, len(lists))
		for _, list := range lists {
			out = append(out, todoListJSON(list))
		}
		printJSON(out)
		return
	}
	if len(lists) == 0 {
		fmt.Println("no lists")
		return
	}
	for _, list := range lists {
		state := ""
		if list.DeletedAt != nil {
			state = " deleted"
		}
		style := ""
		if list.Color != "" || list.Icon != "" {
			style = fmt.Sprintf(" color=%s icon=%s", list.Color, list.Icon)
		}
		syncSuffix := ""
		if list.LastSyncError != "" {
			syncSuffix = " sync_error=" + list.LastSyncError
		}
		fmt.Printf("%s %s%s%s%s\n", shortID(list.UUID), list.Name, state, style, syncSuffix)
	}
}

func todoJSON(todo integlife.TodoRecord) map[string]any {
	return map[string]any{
		"uuid":            todo.UUID,
		"content":         todo.Content,
		"notes":           todo.Notes,
		"completed":       todo.Completed,
		"order":           todo.SortOrder,
		"list_uuid":       todo.ListUUID,
		"completed_at":    formatTimePtr(todo.CompletedAt),
		"deleted_at":      formatTimePtr(todo.DeletedAt),
		"updated_at":      todo.ClientUpdatedAt.Format(time.RFC3339Nano),
		"last_sync_error": todo.LastSyncError,
	}
}

func todoListJSON(list integlife.TodoListRecord) map[string]any {
	return map[string]any{
		"uuid":            list.UUID,
		"name":            list.Name,
		"color":           list.Color,
		"icon":            list.Icon,
		"sort_order":      list.SortOrder,
		"deleted_at":      formatTimePtr(list.DeletedAt),
		"updated_at":      list.ClientUpdatedAt.Format(time.RFC3339Nano),
		"last_sync_error": list.LastSyncError,
	}
}

func formatTimePtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func shortID(uuid string) string {
	if len(uuid) <= 8 {
		return uuid
	}
	return uuid[:8]
}

func printJSON(value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Printf(`{"ok":false,"error":"%s"}`+"\n", err.Error())
		return
	}
	fmt.Println(string(data))
}

func flagProvided(fs *flag.FlagSet, name string) bool {
	provided := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}

func parseCommandFlags(fs *flag.FlagSet, args []string, valueFlagNames []string, boolFlagNames []string) ([]string, error) {
	valueFlags := make(map[string]bool, len(valueFlagNames))
	for _, name := range valueFlagNames {
		valueFlags[name] = true
	}
	boolFlags := make(map[string]bool, len(boolFlagNames))
	for _, name := range boolFlagNames {
		boolFlags[name] = true
	}

	flagArgs := []string{}
	positionals := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "--") && len(arg) > 2 {
			nameWithValue := strings.TrimPrefix(arg, "--")
			name, _, hasValue := strings.Cut(nameWithValue, "=")
			switch {
			case valueFlags[name]:
				flagArgs = append(flagArgs, arg)
				if !hasValue {
					if i+1 >= len(args) {
						return nil, fmt.Errorf("--%s requires a value", name)
					}
					i++
					flagArgs = append(flagArgs, args[i])
				}
				continue
			case boolFlags[name]:
				flagArgs = append(flagArgs, arg)
				continue
			}
		}
		positionals = append(positionals, arg)
	}
	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}
	return positionals, nil
}

func printUsage(out *os.File) {
	moods := make([]string, len(validMoods))
	for i, m := range validMoods {
		moods[i] = string(m)
	}
	fmt.Fprintf(out, `usage:
  life '<content>'                      record a thought (default category)
  life <category> '<content>'           record with a specific category
  life ... --mood <mood>                attach a mood to the entry

  life login                            authenticate and save token
  life logout                           remove saved token
  life sync                             push pending entries to the server
  life todo <command>                   manage todos
  life list <command>                   manage todo lists
  life ai <command>                     report AI task progress

moods: %s

examples:
  life 'had a great walk today'
  life ai 'GPT-4 is getting interesting'
  life --mood happy 'shipped the feature!'
  life work --mood focused 'deep work session'
`, strings.Join(moods, ", "))
}

func printTodoUsage(out *os.File) {
	fmt.Fprint(out, `usage:
  life todo add [--notes <text>] [--list <list>] [--order <n>] <content>
  life todo list [--open|--done] [--all] [--list <list>] [--json]
  life todo show <uuid-or-prefix> [--json]
  life todo update <uuid-or-prefix> [--content <text>] [--notes <text>] [--list <list>] [--clear-list] [--order <n>] [--done|--open]
  life todo done <uuid-or-prefix> [--undo]
  life todo delete <uuid-or-prefix>
`)
}

func printTodoListUsage(out *os.File) {
	fmt.Fprint(out, `usage:
  life list add [--color <value>] [--icon <value>] [--order <n>] <name>
  life list list [--all] [--json]
  life list update <uuid-prefix-or-name> [--name <text>] [--color <value>] [--icon <value>] [--order <n>]
  life list delete <uuid-prefix-or-name>
`)
}

func printAIUsage(out *os.File) {
	fmt.Fprint(out, `usage:
  life ai start --project goal:<uuid> --todo <uuid> --title <text> --agent codex --json
  life ai progress --run <uuid> --phase <phase> --summary <text>
  life ai heartbeat --run <uuid> --summary <text>
  life ai event --run <uuid> --type artifact --title <title> --content <text> --metadata-json '{}'
  life ai block --run <uuid> --question <text>
  life ai complete --run <uuid> --summary <text> --artifact <path>
  life ai status --run <uuid>
  life ai status --current --project goal:<uuid> --todo <uuid>
  life ai resume --project goal:<uuid> --todo <uuid> --agent codex --json
  life ai resolve --run <uuid> --prefer-local
`)
}
