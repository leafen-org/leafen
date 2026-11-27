package app

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/spf13/cobra"
)

// `leafen setup sign-in`, in the desktop app's program, signs the app in to
// a Leafen used already: from its first screen, or past it as another
// Leafen beside the ones it has, as its account menu's "Add an account"
// does. With a one-time code the person got from a device signed in there
// (the workspace name in the sidebar › Add a device) or from that server's
// administrator (`leafen account device-code`), the app's window signs in
// with it and opens that Leafen, and nothing is left for the person to do in
// the window (user decision 2026-11-13: the first run and adding an account
// go through the agent's conversation, signing in included); the command
// says whom it signed in as, or why the code was refused, so the agent can
// ask for another. On the first screen without a code, the window shows that
// Leafen's sign-in for the person; past it, a code is needed. The code goes to the
// window in the request file beside the app's data, which the window takes
// within seconds and removes once it has answered; this command removes it
// too when the window doesn't answer, and prints it nowhere.

const (
	// Long enough for the window to look (every 1.5 s), find the server and sign in.
	setupSignInWait = 45 * time.Second
	// Without a code, until the window has opened the server's sign-in.
	setupSignInShowWait = 20 * time.Second
)

func setupSignInCommand(pb *pocketbase.PocketBase) *cobra.Command {
	var code string
	var signOutLeastUsed bool
	command := &cobra.Command{
		Use:   "sign-in <invite link or address>",
		Short: "In the app: log in to a Leafen used already, with a one-time code from a device logged in there",
		Long: "Logs the app in from its first screen, or past it adds another Leafen beside the ones it has (as the account menu's " +
			"\"Add an account\" does). With --code, the one-time code the person got from a device logged in to that Leafen (the " +
			"workspace name in the sidebar › Add a device) or from its administrator (`leafen account device-code`), the app logs in " +
			"with it and opens that Leafen; it says whom it logged in as, or why the code was refused. A device code's own link works as " +
			"the address. On the first screen without a code, the app's window shows that Leafen's login for the person.",
		Args: cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupSignIn(cmd.OutOrStdout(), pb.DataDir(), args[0], code, signOutLeastUsed)
		},
	}
	command.Flags().StringVar(&code, "code", "", "the one-time code (twelve digits) the person got from a device logged in there")
	command.Flags().BoolVar(&signOutLeastUsed, "sign-out-least-used", false,
		"where the account is logged in on as many devices as it may be: log out its least recently used one for this app, "+
			"once the person said yes to it in the conversation")
	return command
}

func runSetupSignIn(out io.Writer, dir, address, code string, signOutLeastUsed bool) error {
	if setupApp == nil {
		return errors.New("logging in is the app's; in a browser, open the address and log in there")
	}
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("give the invite link or the Leafen address the person has, such as https://wiki.example.com")
	}
	// A device code's own link (<server>/device/<code>) carries its code.
	if linked, found := strings.CutPrefix(parsed.Path, deviceCodePagePath); found && strings.TrimSpace(code) == "" {
		code, parsed.Path, parsed.RawQuery, parsed.Fragment = linked, "", "", ""
	}
	request := setupRequest{SignIn: parsed.String()}
	if strings.TrimSpace(code) != "" {
		normalized, ok := normalizeDeviceCode(code)
		if !ok {
			return errors.New("a code is the twelve digits a device logged in there shows under the workspace name in the sidebar › " +
				"Add a device, or that `leafen account device-code` gives on that server")
		}
		request.Code, request.SignOutLeastUsed = normalized, signOutLeastUsed
	}
	// Past the first screen the app has a Leafen already: this one is added beside it.
	adding := setupApp.Choice() != ""
	if adding && request.Code == "" {
		return errors.New("the app has a Leafen already, so this adds another, which takes a one-time code: ask the person for one " +
			"from a device logged in there (the workspace name in the sidebar › Add a device) and run this again with --code")
	}
	if err := leaveSetupRequest(dir, &request); err != nil {
		return err
	}
	host := parsed.Host
	if request.Code == "" {
		for deadline := time.Now().Add(setupSignInShowWait); time.Now().Before(deadline); time.Sleep(time.Second) {
			if setupApp.Choice() == "server" {
				fmt.Fprintf(out, "The Leafen window now shows the login of %s. The person logs in there, in the ways that Leafen offers.\n", host)
				return nil
			}
		}
		fmt.Fprintln(out, "The Leafen window didn't take the address: is the app open? It says why on its login screen if the address is wrong.")
		return nil
	}
	for deadline := time.Now().Add(setupSignInWait); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		account, refused, answered := setupApp.SignedIn(request.At)
		if !answered {
			continue
		}
		if refused != "" {
			return errors.New(setupSignInRefusal(host, refused))
		}
		if adding {
			fmt.Fprintf(out, "The app is logged in to %s as %s too, and its window opens that Leafen; the account menu has the ones "+
				"it had, as before.\n", host, account)
		} else {
			fmt.Fprintf(out, "The app is logged in to %s as %s, and its window opens that Leafen.\n", host, account)
			fmt.Fprintln(out, "That Leafen's administrator set it up, so nothing of the first run is left here.")
		}
		fmt.Fprintf(out, "Its MCP address, to connect yourself there: %s://%s%s\n", parsed.Scheme, host, nativeMCPPath)
		return nil
	}
	// Taken back whole, code and all: the agent runs it again once the app is open.
	request.SignIn, request.Code = "", ""
	_ = writeSetupFile(dir, setupRequestFile, request)
	if adding {
		return errors.New("no Leafen window took the code: is the app open? Run this again once it is")
	}
	return errors.New("the Leafen window didn't take the code: is the app open on its first screen? Run this again once it is")
}

// setupSignInRefusal says why the window couldn't sign in, and what the agent does next.
func setupSignInRefusal(host, refused string) string {
	again := "ask the person for a new code (a device logged in there: the workspace name in the sidebar › Add a device) and run this again"
	switch refused {
	case "address":
		return fmt.Sprintf("no Leafen answers at %s: check the address with the person", host)
	case "plain":
		return fmt.Sprintf("the app connects to %s only at a secure address (https://): ask the person for that server's https address", host)
	case "LEAFEN_TRANSPORT_UNAVAILABLE":
		return fmt.Sprintf("the app couldn't reach %s to log in: check with the person that it is up and the address is right, "+
			"then run this again (the same code works for its ten minutes)", host)
	case "keep":
		return "the app couldn't keep the login on this computer; run this again without --code, and the person logs in in the app's window"
	case errorCodeDeviceCodeInvalid:
		return fmt.Sprintf("%s didn't take the code: it isn't one that server gave, or it was used or ran out (a code is good once, "+
			"for ten minutes). Don't try it again: %s", host, again)
	case errorCodeDeviceCodeLimited:
		return fmt.Sprintf("%s refused the code: too many codes were tried there. Wait a quarter of an hour, then %s", host, again)
	case errorCodeAuthSessionCapacity:
		return fmt.Sprintf("the account is logged in on as many devices as %s allows. Ask the person whether to log out its least "+
			"recently used one for this app; only on their yes, run this again with --sign-out-least-used (the same code works for its "+
			"ten minutes). If they say no, they log out on one of their devices first", host)
	case errorCodeSAMLRequired:
		return "that account's organization logs it in with its single sign-on, not a code: run this again without --code, and the " +
			"person logs in in the app's window"
	case errorCodeDeviceSignInRestricted:
		return "that account's organization asks for a password and 2-step verification, not a code: run this again without --code, " +
			"and the person logs in in the app's window"
	}
	return fmt.Sprintf("the app couldn't log in to %s (%s); its window says why. %s", host, refused, strings.ToUpper(again[:1])+again[1:])
}
