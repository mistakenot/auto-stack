package mail

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-mail/internal/config"
	"github.com/mistakenot/auto-mail/internal/store"
)

// openStore is the package's only door to the mail store. It is a package var
// so a test can count the opens a code path performs, which is how AC-10's
// central claim — the hook's mail check opens the store on *neither* path — is
// asserted rather than assumed.
var openStore = store.Open

// nudgeText is the fixed instruction a working agent is handed in-band when it
// has mail waiting.
//
// It is a constant, and deliberately so: this string lands in another agent's
// context window, and the flag that triggers it is set by whoever sent the
// mail. Interpolating a sender, an address, a subject or a body would make the
// sender an author of the recipient's context (G14's content rule, adopted for
// the in-band channel too). The nudge says only "there is mail, go and read
// it" — every byte of content still travels through `auto mail list`.
const nudgeText = "You have unread agent mail. Run `auto mail list` to read it, " +
	"then `auto mail ack <id>` for each item once you have acted on it. " +
	"Reading never retires mail, so the ack is always a separate call."

// NudgeText is the fixed instruction emitted to a working agent that has mail.
// Never interpolated from an envelope, a body or a sender.
func NudgeText() string { return nudgeText }

// flagName is the per-binding flag filename: the first 8 bytes of
// sha256(manager + "\x00" + target), hex-encoded.
//
// Hashing rather than escaping is what makes the name safe: a binding target is
// a tmux pane id or an absolute path, neither of which is a legal filename, and
// the NUL separator means (`a`, `bc`) and (`ab`, `c`) can never collide on one
// flag. The session is excluded on purpose — it is context on the binding row,
// not part of the identity the store joins on.
func flagName(b Binding) string {
	sum := sha256.Sum256([]byte(b.Manager + "\x00" + b.Target))
	return hex.EncodeToString(sum[:8])
}

// flagPath is the single path HasPending stats.
func flagPath(home string, b Binding) string {
	return filepath.Join(config.FlagsDirIn(home), flagName(b))
}

// addressable reports whether a binding names anything at all. An empty pair
// hashes to a real filename that every empty binding would share, so it is
// refused rather than allowed to become a global flag.
func addressable(home string, b Binding) bool {
	return home != "" && b.Manager != "" && b.Target != ""
}

// HasPending reports whether this binding has mail waiting, at the cost of
// exactly one os.Stat and no store open (G8/D-062-3).
//
// Every error — a missing flag directory, an unreadable one, a home that cannot
// be resolved — reads as "no mail". There is no error return by design: this
// runs on every tool call of every agent on the host, and a caller in that
// position has nothing useful to do with a failure except ignore it. A false
// negative delays the nudge until the next send; a false positive costs one
// wasted `auto mail list`, which finds nothing and clears the flag itself.
func HasPending(home string, b Binding) bool {
	if !addressable(home, b) {
		return false
	}
	_, err := os.Stat(flagPath(home, b))
	return err == nil
}

// setPending raises this binding's flag. Called on send, once per bound
// subscription that the mail was delivered to.
//
// The flag is a hint about *state*, not a second copy of the mailbox: it says
// "there may be something here", and the store remains the only authority on
// what. A failure to write one is therefore not a failure to send — the mail is
// already committed — so the error is returned for the caller to swallow
// knowingly rather than raised through the send path.
func setPending(home string, b Binding) error {
	if !addressable(home, b) {
		return nil
	}
	path := flagPath(home, b)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// clearPending lowers this binding's flag. Called after a list or an ack that
// leaves the binding's subscriptions with nothing unacked — the self-healing
// half of the loop that bounds flag drift (D-062-3).
func clearPending(home string, b Binding) error {
	if !addressable(home, b) {
		return nil
	}
	if err := os.Remove(flagPath(home, b)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
