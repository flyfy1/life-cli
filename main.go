package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/songyy/statuslog-go/internal/statuslog"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg := statuslog.LoadConfig()

	store, err := statuslog.OpenStore(cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	client := statuslog.NewClient(cfg.APIURL, cfg.APIToken, 10*time.Second)
	service := statuslog.NewService(store, client)

	if len(args) == 0 {
		printUsage(os.Stderr)
		return errors.New("missing command")
	}

	switch args[0] {
	case "log":
		if len(args) != 3 {
			printUsage(os.Stderr)
			return errors.New("log requires <type> and <content>")
		}

		logType := strings.TrimSpace(args[1])
		content := strings.TrimSpace(args[2])
		if logType == "" || content == "" {
			return errors.New("type and content must be non-empty")
		}

		record, synced, syncDetail, err := service.LogAndMaybeSync(ctx, logType, content)
		if err != nil {
			return err
		}

		fmt.Printf("saved locally: uuid=%s type=%s\n", record.UUID, record.LogType)
		if synced {
			fmt.Printf("sync ok: %s\n", syncDetail)
			return nil
		}

		if syncDetail != "" {
			fmt.Printf("sync skipped or failed: %s\n", syncDetail)
		}
		return nil

	case "sync":
		result, err := service.SyncPending(ctx)
		if err != nil {
			return err
		}

		fmt.Printf("sync finished: synced=%d pending=%d\n", result.Synced, result.Pending)
		if len(result.Failures) > 0 {
			for _, failure := range result.Failures {
				fmt.Printf("sync failed: uuid=%s detail=%s\n", failure.UUID, failure.Detail)
			}
		}
		return nil

	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil

	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func printUsage(out *os.File) {
	fmt.Fprintln(out, `usage:
  statuslog log <type> "<content>"
  statuslog sync`)
}
