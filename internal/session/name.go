package session

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// Human-recognizable session names are "adjective-noun" pairs (e.g.
// "calm-otter"), so switching between sessions is about memorable labels rather
// than hex ids. ~50×50 = 2500 combinations, collision-avoided against sessions
// already known on this machine.

var nameAdjectives = []string{
	"amber", "brave", "bright", "calm", "clever", "cosmic", "crisp", "curious",
	"dapper", "eager", "electric", "fancy", "fuzzy", "gentle", "gilded", "glad",
	"golden", "happy", "hidden", "jolly", "keen", "lucky", "lunar", "mellow",
	"merry", "mighty", "nimble", "noble", "polar", "proud", "quiet", "rapid",
	"royal", "rustic", "sable", "shiny", "silent", "silver", "sleek", "snug",
	"solar", "spry", "stout", "sunny", "swift", "tidy", "vivid", "witty",
	"zesty", "zippy",
}

var nameNouns = []string{
	"otter", "falcon", "maple", "cinder", "harbor", "pebble", "willow", "raven",
	"comet", "meadow", "badger", "cobra", "ferret", "gecko", "heron", "ibex",
	"jaguar", "koala", "lynx", "marmot", "newt", "osprey", "panda", "quail",
	"rabbit", "seal", "tiger", "urchin", "viper", "walrus", "yak", "zebra",
	"anchor", "beacon", "canyon", "delta", "ember", "fjord", "grove", "hollow",
	"island", "juniper", "kettle", "lagoon", "mesa", "nebula", "orchard",
	"prairie", "quartz", "ridge",
}

// nameRE is the allowed shape for a (user-supplied) session name: a short,
// lowercase, filesystem- and shell-safe token. Generated names always match.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,30}$`)

// ValidName reports whether s is an acceptable session name.
func ValidName(s string) bool { return nameRE.MatchString(s) }

// GenerateName returns a memorable "adjective-noun" name not present in taken.
// After a few collisions it appends a short numeric suffix to guarantee
// termination even if the space is crowded.
func GenerateName(taken map[string]bool) string {
	for attempt := 0; attempt < 40; attempt++ {
		n := pick(nameAdjectives) + "-" + pick(nameNouns)
		if !taken[n] {
			return n
		}
	}
	for i := 2; ; i++ {
		n := fmt.Sprintf("%s-%s-%d", pick(nameAdjectives), pick(nameNouns), i)
		if !taken[n] {
			return n
		}
	}
}

// TakenNames returns the set of names already assigned to known sessions, so a
// new name doesn't collide with an existing one on this machine.
func TakenNames() map[string]bool {
	taken := map[string]bool{}
	metas, err := List()
	if err != nil {
		return taken
	}
	for _, m := range metas {
		if m.Name != "" {
			taken[strings.ToLower(m.Name)] = true
		}
	}
	return taken
}

func pick(list []string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		return list[0]
	}
	return list[n.Int64()]
}
