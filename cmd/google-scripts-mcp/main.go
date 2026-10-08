// Command google-scripts-mcp is a stdio MCP server for taking stock of one
// Google account's Apps Script projects: which exist, which still run, what
// fails, and the code, pulled into a local directory to fix.
//
//	google-scripts-mcp login -account you@gmail.com           sign in, once, in a terminal
//	google-scripts-mcp whoami                                  which account the saved sign-in is for
//	google-scripts-mcp                                         the MCP server, on stdin/stdout
//
// See README.md for the one-time Google Cloud setup the login needs.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jroedel/google-scripts-mcp/internal/auth"
	"github.com/jroedel/google-scripts-mcp/internal/tools"
)

const version = "0.1.0"

func main() {
	log.SetFlags(0)
	log.SetPrefix("google-scripts-mcp: ")

	dir, err := auth.Dir()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "login":
		fs := flag.NewFlagSet("login", flag.ExitOnError)
		account := fs.String("account", "", "the Google account to sign in as; the sign-in is refused for any other")
		fs.Parse(os.Args[2:])
		if *account == "" {
			log.Fatal("say which account: google-scripts-mcp login -account <gmail address>")
		}
		if err := auth.Login(ctx, dir, *account, os.Stderr); err != nil {
			log.Fatal(err)
		}

	case "whoami":
		s, err := auth.Open(ctx, dir)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(s.Account)
		if missing := auth.Missing(s.Scopes); len(missing) > 0 {
			fmt.Println("not granted:", strings.Join(missing, " "))
		}

	case "", "serve":
		serve(ctx, dir)

	default:
		log.Fatalf("unknown command %q. Commands: login, whoami, serve (the default)", cmd)
	}
}

func serve(ctx context.Context, dir string) {
	// stdout is the protocol. Everything else goes to stderr, which is what
	// log does by default, and nothing in this process may print.
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "google-scripts",
		Title:   "Google Apps Script",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: "Read-only tools for taking stock of the Apps Script projects in one Google account, " +
			"plus script_pull to bring a script's code into a local directory to fix under git. Start " +
			"with scripts_inventory. Nothing here can change, deploy or delete a script on Google's side.",
	})
	tools.Register(s, &tools.Deps{Dir: dir, NewClient: tools.OpenSession(dir)})

	if err := s.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
