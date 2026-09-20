package demo

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var publishedConversation = [][2]string{
	{"Men are all alike.", "IN WHAT WAY"},
	{"They're always bugging us about something or other.", "CAN YOU THINK OF A SPECIFIC EXAMPLE"},
	{"Well, my boyfriend made me come here.", "YOUR BOYFRIEND MADE YOU COME HERE"},
	{"He says I'm depressed much of the time.", "I AM SORRY TO HEAR YOU ARE DEPRESSED"},
	{"It's true. I am unhappy.", "DO YOU THINK COMING HERE WILL HELP YOU NOT TO BE UNHAPPY"},
	{"I need some help, that much seems certain.", "WHAT WOULD IT MEAN TO YOU IF YOU GOT SOME HELP"},
	{"Perhaps I could learn to get along with my mother.", "TELL ME MORE ABOUT YOUR FAMILY"},
	{"My mother takes care of me.", "WHO ELSE IN YOUR FAMILY TAKES CARE OF YOU"},
	{"My father.", "YOUR FATHER"},
	{"You are like my father in some ways.", "WHAT RESEMBLANCE DO YOU SEE"},
	{"You are not very aggressive but I think you don't want me to notice that.", "WHAT MAKES YOU THINK I AM NOT VERY AGGRESSIVE"},
	{"You don't argue with me.", "WHY DO YOU THINK I DON'T ARGUE WITH YOU"},
	{"You are afraid of me.", "DOES IT PLEASE YOU TO BELIEVE I AM AFRAID OF YOU"},
	{"My father is afraid of everybody.", "WHAT ELSE COMES TO MIND WHEN YOU THINK OF YOUR FATHER"},
	{"Bullies.", "DOES THAT HAVE ANYTHING TO DO WITH THE FACT THAT YOUR BOYFRIEND MADE YOU COME HERE"},
}

func newTestDoctor(t *testing.T) *doctor {
	t.Helper()

	eliza, err := consultDoctor()
	if err != nil {
		t.Fatal(err)
	}

	return eliza
}

func TestTheScriptIsTheOneWeizenbaumPublished(t *testing.T) {
	loaded, err := loadScript()
	if err != nil {
		t.Fatal(err)
	}

	if want := "HOW DO YOU DO. PLEASE TELL ME YOUR PROBLEM"; loaded.greeting != want {
		t.Errorf("the script opens with %q, want %q", loaded.greeting, want)
	}
	if loaded.memoryTrigger != "MY" || len(loaded.memories) != 4 {
		t.Errorf("the script remembers %d things about %q", len(loaded.memories), loaded.memoryTrigger)
	}
	if rank := loaded.rules["COMPUTER"].rank; rank != 50 {
		t.Errorf("COMPUTER ranks %d, want 50", rank)
	}
	if substitute := loaded.rules["MY"].substitute; substitute != "YOUR" {
		t.Errorf("MY substitutes %q, want %q", substitute, "YOUR")
	}
	if redirect := loaded.rules["ALIKE"].redirect; redirect != "DIT" {
		t.Errorf("ALIKE redirects to %q, want %q", redirect, "DIT")
	}
	if tags := loaded.rules["MOTHER"].tags; !slices.Equal(tags, []string{"NOUN", "FAMILY"}) {
		t.Errorf("MOTHER is tagged %v", tags)
	}
	if count := len(loaded.rules["I"].transforms); count != 12 {
		t.Errorf("I carries %d decompositions, want 12", count)
	}
}

func TestEveryRedirectInTheScriptNamesARuleThatExists(t *testing.T) {
	loaded, err := loadScript()
	if err != nil {
		t.Fatal(err)
	}

	named := func(key string) {
		if _, isKnown := loaded.rules[key]; !isKnown {
			t.Errorf("the script sends a reply to %q, which it never defines", key)
		}
	}

	for _, found := range loaded.rules {
		if found.redirect != "" {
			named(found.redirect)
		}

		for _, candidate := range found.transforms {
			for _, picked := range candidate.reassemblies {
				if picked.redirect != "" {
					named(picked.redirect)
				}
				if picked.rewriteKey != "" {
					named(picked.rewriteKey)
				}
			}
		}
	}
}

func TestTheDoctorAnswersTheConversationInThePaper(t *testing.T) {
	eliza := newTestDoctor(t)

	for _, exchange := range publishedConversation {
		if reply := eliza.Reply(exchange[0]); reply != exchange[1] {
			t.Errorf("%q was answered with %q, want %q", exchange[0], reply, exchange[1])
		}
	}
}

func TestTheDoctorAnswersAWholeConversationTheOriginalWouldRecognise(t *testing.T) {
	source, err := os.Open(filepath.Join("testdata", "conversation.txt"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = source.Close() })

	eliza := newTestDoctor(t)
	lines := bufio.NewScanner(source)
	said := ""
	exchanges := 0

	for lines.Scan() {
		line := lines.Text()

		switch {
		case line == "" || strings.HasPrefix(line, ";"):
		case strings.HasPrefix(line, "> "):
			said = strings.TrimPrefix(line, "> ")
		default:
			exchanges++

			if reply := eliza.Reply(said); reply != line {
				t.Errorf("%q was answered with %q, want %q", said, reply, line)
			}
		}
	}

	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	if exchanges < 200 {
		t.Errorf("the conversation is only %d exchanges long", exchanges)
	}
}

func TestTheDoctorHashesAWordTheWayWeizenbaumSaidItDid(t *testing.T) {
	if got := hash(0o214366217062, 7); got != 14 {
		t.Errorf("ALWAYS hashed to %d, want the 14 Weizenbaum published", got)
	}

	tests := map[string]int{
		"PURPOSE":         1,
		"DEVONSHIRE":      0,
		"PREDICAMENT":     3,
		"EXECUTIONERS":    3,
		"GLOUCESTERSHIRE": 2,
	}

	for word, want := range tests {
		if got := hash(lastChunkAsBCD(word), memoryBits); got != want {
			t.Errorf("%s hashed to %d, want %d", word, got, want)
		}
	}
}

func TestTheDoctorFallsBackOnItsBuiltInWordsWhenNoPatternFits(t *testing.T) {
	eliza := newTestDoctor(t)

	if reply := eliza.Reply("Je parle francais."); reply != "I AM SORRY, I SPEAK ONLY ENGLISH" {
		t.Errorf("the doctor answered %q", reply)
	}
	if !slices.Contains(confusions, eliza.Reply("Can.")) {
		t.Errorf("a keyword whose patterns cannot match produced no built-in reply")
	}
}

func TestTheDoctorFallsBackOnItsOwnWordsWithNothingToRemember(t *testing.T) {
	eliza := newTestDoctor(t)

	if reply := eliza.Reply("the quarterly figures look wrong"); reply != "I AM NOT SURE I UNDERSTAND YOU FULLY" {
		t.Errorf("the doctor answered %q", reply)
	}
}

func TestTheDoctorWorksThroughItsRepliesBeforeRepeatingOne(t *testing.T) {
	eliza := newTestDoctor(t)

	var replies []string

	for range 5 {
		replies = append(replies, eliza.Reply("the quarterly figures look wrong"))
	}

	for at, reply := range replies[:4] {
		if slices.Contains(replies[:at], reply) {
			t.Errorf("the doctor said %q twice before working through the others", reply)
		}
	}
	if replies[4] != replies[0] {
		t.Errorf("the doctor came back to %q rather than %q", replies[4], replies[0])
	}
}

func TestTheDoctorAnswersTheHighestRankedKeyword(t *testing.T) {
	eliza := newTestDoctor(t)

	if reply := eliza.Reply("I think computers are wonderful"); reply != "DO COMPUTERS WORRY YOU" {
		t.Errorf("the doctor answered %q, want the reply COMPUTER earns at rank 50", reply)
	}
}

func TestTheDoctorKeepsToThePartOfTheSentenceWithTheKeyword(t *testing.T) {
	eliza := newTestDoctor(t)

	want := "YOUR HANDS SHAKE"
	if reply := eliza.Reply("It has been a long day, my hands shake, and I am tired."); reply != want {
		t.Errorf("the doctor answered %q, want %q", reply, want)
	}
}

func TestTheDoctorTurnsWhatWasSaidBackOnTheOneWhoSaidIt(t *testing.T) {
	eliza := newTestDoctor(t)

	want := "WHY DO YOU THINK I NEVER ANSWER YOU"
	if reply := eliza.Reply("You never answer me"); reply != want {
		t.Errorf("the doctor answered %q, want %q", reply, want)
	}
}

func TestTheDoctorSaysSomethingToAnythingAtAll(t *testing.T) {
	eliza := newTestDoctor(t)

	for _, said := range []string{"", "...", "?", "hmm", "42", "I'm no one", "WHY NOT"} {
		if reply := eliza.Reply(said); strings.TrimSpace(reply) == "" {
			t.Errorf("the doctor said nothing to %q", said)
		}
	}
}

func TestTheDoctorIsHeardWithoutShouting(t *testing.T) {
	tests := map[string]string{
		"IN WHAT WAY": "In what way?",
		"WHY DO YOU THINK I DON'T ARGUE WITH YOU":  "Why do you think I don't argue with you?",
		"I'M SURE ITS NOT PLEASANT TO BE SAD":      "I'm sure its not pleasant to be sad.",
		"ARE YOU SAYING 'NO' JUST TO BE NEGATIVE":  "Are you saying 'no' just to be negative?",
		"WE WERE DISCUSSING YOU - NOT ME":          "We were discussing you - not me.",
		"GO ON , PLEASE":                           "Go on, please.",
		"WHO, MAY I ASK":                           "Who, may I ask?",
		"OF WHAT DOES FEELING SAD REMIND YOU":      "Of what does feeling sad remind you?",
		"PERHAPS I ALREADY KNEW YOU WERE SAD":      "Perhaps I already knew you were sad.",
		"YOU LIKE TO THINK I HATE YOU - DON'T YOU": "You like to think I hate you - don't you?",
		"HOW DO YOU DO. PLEASE STATE YOUR PROBLEM": "How do you do. Please state your problem.",
	}

	for shouted, want := range tests {
		if spoken := speak(shouted); spoken != want {
			t.Errorf("%q was said as %q, want %q", shouted, spoken, want)
		}
	}
}
