package main

import (
	"context"
	"errors"
	"os"
	"os/signal"

	"github.com/jessevdk/go-flags"
)

const version = "dev"

//nolint:gochecknoglobals
var global context.Context // global context for main package only!

func main() {
	var app struct {
		Run CommandRun `command:"run" description:"(default) run git-pipe and serve repos"`
		JWT CommandJWT `command:"jwt" description:"helper to generate JWT"`
	}

	parser := flags.NewParser(&app, flags.Default)
	parser.LongDescription = "Watch and deploy docker-based applications from Git\nAuthor: Baryshnikov Aleksandr <dev@baryshnikov.net>\nVersion: " + version

	ctx, closer := signal.NotifyContext(context.Background(), os.Interrupt)
	global = ctx

	args := os.Args[1:]
	_, err := parser.ParseArgs(args)

	var flagsErr *flags.Error
	if errors.As(err, &flagsErr) && (flagsErr.Type == flags.ErrCommandRequired || flagsErr.Type == flags.ErrUnknownCommand) {
		args = append([]string{"run"}, args...)
		_, err = parser.ParseArgs(args)
	}
	closer()

	if err != nil && !(errors.As(err, &flagsErr) && flagsErr.Type == flags.ErrHelp) {
		os.Exit(1)
	}
}
