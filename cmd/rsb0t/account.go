package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"rsb0t-go/internal/bot"
)

// account implements `rsb0t account add|list|script|remove`.
func account(home string, args []string) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	r, err := ensureRoster(home)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t:", err)
		return 1
	}
	switch args[0] {
	case "list", "ls":
		list := r.accounts()
		if len(list) == 0 {
			fmt.Println("no accounts; add one with `rsb0t account add NAME`")
			return 0
		}
		creds, _ := loadCredentials(home)
		for _, a := range list {
			have := "password stored"
			if creds.passwordFor(a.name()) == "" {
				have = "NO PASSWORD (rsb0t account add " + a.name() + ")"
			}
			s := a.script()
			if s == "" {
				s = "(no script)"
			}
			fmt.Printf("%-12s  %-24s  %s\n", a.name(), s, have)
		}
		return 0
	case "add":
		return accountAdd(home, r, args[1:])
	case "script":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: rsb0t account script NAME [SCRIPT]   (see `rsb0t scripts`)")
			return 2
		}
		script := ""
		if len(args) > 2 {
			script = args[2]
		}
		if script != "" && !scriptKnown(script) {
			fmt.Fprintf(os.Stderr, "rsb0t: no script called %q; `rsb0t scripts` lists them\n", script)
			return 1
		}
		return setScript(r, args[1], script)
	case "remove", "rm", "delete":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: rsb0t account remove NAME")
			return 2
		}
		i := r.find(args[1])
		if i < 0 {
			fmt.Fprintf(os.Stderr, "rsb0t: no account %q\n", args[1])
			return 1
		}
		list := r.accounts()
		list = append(list[:i], list[i+1:]...)
		r.setAccounts(list)
		if err := r.save(); err != nil {
			fmt.Fprintln(os.Stderr, "rsb0t:", err)
			return 1
		}
		creds, err := loadCredentials(home)
		if err == nil {
			creds.delete(args[1])
			err = creds.save(home)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "rsb0t: removed from the roster, but not the stored password:", err)
			return 1
		}
		fmt.Printf("removed %s and its stored password\n", args[1])
		return 0
	}
	fmt.Fprintf(os.Stderr, "rsb0t: unknown account command %q (add, list, script, remove)\n", args[0])
	return 2
}

func scriptKnown(name string) bool {
	for _, n := range bot.Names() {
		if n == name {
			return true
		}
	}
	return false
}

func setScript(r *roster, name, script string) int {
	i := r.find(name)
	if i < 0 {
		fmt.Fprintf(os.Stderr, "rsb0t: no account %q\n", name)
		return 1
	}
	list := r.accounts()
	b, _ := json.Marshal(script)
	list[i]["script"] = b
	r.setAccounts(list)
	if err := r.save(); err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t:", err)
		return 1
	}
	if script == "" {
		script = "(no script: connect only)"
	}
	fmt.Printf("%s starts with %s\n", list[i].name(), script)
	return 0
}

// errNoTerminal is readSecret's answer when the input is not a keyboard: a password piped in by accident
// (`echo x | rsb0t account add NAME`) is refused unless -password-stdin says that was meant.
var errNoTerminal = errors.New("the input is not a keyboard, so the password cannot be typed with its echo off; " +
	"type it in a terminal window, add the account on the setup page, or pass -password-stdin if piping it in is really meant")

// accountAdd is `rsb0t account add NAME [-script S] [-password-env VAR | -password-stdin]`. With neither
// password option the password is asked for with the terminal's echo off.
func accountAdd(home string, r *roster, args []string) int {
	var name, script, envVar string
	fromStdin := false
	for i := 0; i < len(args); i++ {
		switch a := strings.TrimLeft(args[i], "-"); {
		case a == "script" && i+1 < len(args):
			script = args[i+1]
			i++
		case a == "password-env" && i+1 < len(args):
			envVar = args[i+1]
			i++
		case a == "password-stdin":
			fromStdin = true
		case strings.HasPrefix(args[i], "-"):
			fmt.Fprintf(os.Stderr, "rsb0t: unknown option %s\n", args[i])
			return 2
		case name == "":
			name = args[i]
		default:
			fmt.Fprintf(os.Stderr, "rsb0t: unexpected %q\n", args[i])
			return 2
		}
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "usage: rsb0t account add NAME [-script SCRIPT] [-password-env VAR | -password-stdin]")
		return 2
	}
	if why := checkName(name); why != "" {
		fmt.Fprintf(os.Stderr, "rsb0t: %q %s\n", name, why)
		return 1
	}
	if r.find(name) >= 0 {
		fmt.Fprintf(os.Stderr, "rsb0t: %s is already in the roster; `rsb0t account remove %s` first to change it\n", name, name)
		return 1
	}
	if script != "" && !scriptKnown(script) {
		fmt.Fprintf(os.Stderr, "rsb0t: no script called %q; `rsb0t scripts` lists them\n", script)
		return 1
	}

	var password string
	switch {
	case envVar != "":
		password = os.Getenv(envVar)
		if password == "" {
			fmt.Fprintf(os.Stderr, "rsb0t: %s is empty or unset\n", envVar)
			return 1
		}
	case fromStdin:
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		password = strings.TrimRight(line, "\r\n")
	default:
		p, err := readSecret("password for " + name + ": ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "rsb0t: reading the password:", err)
			return 1
		}
		password = p
	}
	if password == "" {
		fmt.Fprintln(os.Stderr, "rsb0t: the password is empty; nothing was added")
		return 1
	}

	creds, err := loadCredentials(home)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t:", err)
		return 1
	}
	creds[name] = password
	if err := creds.save(home); err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t:", err)
		return 1
	}
	entry := accountEntry{}
	entry["username"], _ = json.Marshal(name)
	entry["script"], _ = json.Marshal(script)
	entry["world"] = json.RawMessage("0")
	entry["settings"] = json.RawMessage("{}")
	r.setAccounts(append(r.accounts(), entry))
	if err := r.save(); err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t:", err)
		return 1
	}
	fmt.Printf("added %s (password kept in %s, readable by you only)\n", name, credentialsFile)
	return 0
}
