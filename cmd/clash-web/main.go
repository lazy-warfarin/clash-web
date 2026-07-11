package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/maris/clash-web/internal/app"
	"github.com/maris/clash-web/internal/helper"
)

var version = "dev"

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC | log.Lmsgprefix)
	log.SetPrefix("clash-web: ")
	command := "serve"
	if len(os.Args) > 1 && os.Args[1][0] != '-' {
		command = os.Args[1]
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	}

	switch command {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		configPath := fs.String("config", "", "path to server config")
		_ = fs.Parse(os.Args[1:])
		cfg, err := app.LoadConfig(*configPath)
		fatal(err)
		server, err := app.New(cfg, version)
		fatal(err)
		defer server.Close()
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		fatal(server.Run(ctx))
	case "helper":
		fs := flag.NewFlagSet("helper", flag.ExitOnError)
		configPath := fs.String("config", "", "path to helper config")
		_ = fs.Parse(os.Args[1:])
		cfg, err := app.LoadConfig(*configPath)
		fatal(err)
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		fatal(helper.Run(ctx, cfg))
	case "reset-password":
		fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
		configPath := fs.String("config", "", "path to server config")
		_ = fs.Parse(os.Args[1:])
		cfg, err := app.LoadConfig(*configPath)
		fatal(err)
		password, err := app.ResetPassword(cfg)
		fatal(err)
		fmt.Printf("New administrator password: %s\n", password)
	case "version", "--version", "-v":
		fmt.Println(version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", command)
		os.Exit(2)
	}
}

func fatal(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
