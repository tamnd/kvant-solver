package ocr

import (
	"os"
	"strings"
	"testing"
)

// answered is a page as it comes off the box: the tool's own header, then the
// transcription. The last two keys are the ones this is all for.
const answered = `---
source: /root/kvant-ocr/in/kvant-1975-1-0050-232dcb/0050.png
model: gpt-5
generated: 2026-08-09T04:11:52Z
elapsed: 63.4s
conversation: https://chatgpt.com/c/68a1f2c0-dead-beef
profile: /root/.config/chatgpt-profile-3
---

A I.31  SUBSETS STABLE UNDER AN ACTION  § 4

The page as the model read it.
`

func TestTheConversationIsReadOutOfTheToolsHeader(t *testing.T) {
	fields := HeaderFields(answered)
	if got := fields["conversation"]; got != "https://chatgpt.com/c/68a1f2c0-dead-beef" {
		t.Errorf("conversation is %q", got)
	}
	if got := fields["profile"]; got != "/root/.config/chatgpt-profile-3" {
		t.Errorf("profile is %q", got)
	}
	if got := fields["model"]; got != "gpt-5" {
		t.Errorf("model is %q", got)
	}
	// The URL has a colon in it, so a reader that split on every colon would
	// have stopped at https.
	if strings.Contains(fields["conversation"], "https") == false {
		t.Error("the url was cut at its own colon")
	}
}

// The tool did not always report these. A volume read before it did has no
// thread, and that is a page which goes back to the image, not a failure.
func TestAnOlderToolsAnswerHasNoConversation(t *testing.T) {
	older := strings.Replace(answered, "conversation: https://chatgpt.com/c/68a1f2c0-dead-beef\nprofile: /root/.config/chatgpt-profile-3\n", "", 1)
	fields := HeaderFields(older)
	if fields == nil {
		t.Fatal("the header itself was not read")
	}
	if fields["conversation"] != "" {
		t.Errorf("a conversation appeared from nowhere: %q", fields["conversation"])
	}
}

// A page that opens with a horizontal rule is a page, not a header. The same
// rule StripToolHeader follows, for the same reason.
func TestAPageIsNotAHeaderBecauseItStartsWithARule(t *testing.T) {
	if fields := HeaderFields("---\ntitle: not the tool\n---\n\nThe page.\n"); fields != nil {
		t.Errorf("a page front matter was read as the tool's header: %v", fields)
	}
}

func TestAThreadIsWrittenWhereTheCorpusIsNot(t *testing.T) {
	root := t.TempDir()
	want := Thread{
		Book: "alg-i-iii", Page: 50, Host: "server3",
		Conversation: "https://chatgpt.com/c/68a1f2c0-dead-beef",
		Profile:      "/root/.config/chatgpt-profile-3",
		Model:        "gpt-5", Read: "2026-08-09T04:11:52Z",
	}
	if err := WriteThread(root, want); err != nil {
		t.Fatal(err)
	}
	// Under work/, which is gitignored. The URL names a chat in somebody's
	// account and the profile is a path in the home directory of a rented box,
	// and neither belongs in a public repository.
	path := ThreadPath(root, "alg-i-iii", 50)
	if !strings.Contains(path, "/work/threads/") {
		t.Errorf("the record is at %s, which is not under work/", path)
	}
	got, err := ReadThread(root, "alg-i-iii", 50)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("the record came back as %+v", got)
	}
}

func TestAPageWithNoRecordSaysSoRatherThanGuessing(t *testing.T) {
	if _, err := ReadThread(t.TempDir(), "alg-i-iii", 50); !os.IsNotExist(err) {
		t.Errorf("a page with no record gave %v", err)
	}
}
