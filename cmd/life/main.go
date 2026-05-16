package main

import (
	"bufio"
	"context"
	"errors"
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
	MoodHappy    Mood = "happy"
	MoodSad      Mood = "sad"
	MoodAnxious  Mood = "anxious"
	MoodCalm     Mood = "calm"
	MoodExcited  Mood = "excited"
	MoodTired    Mood = "tired"
	MoodFocused  Mood = "focused"
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

moods: %s

examples:
  life 'had a great walk today'
  life ai 'GPT-4 is getting interesting'
  life --mood happy 'shipped the feature!'
  life work --mood focused 'deep work session'
`, strings.Join(moods, ", "))
}
