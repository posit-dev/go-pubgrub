// SPDX-License-Identifier: Apache-2.0 OR MIT

package solver

import (
	"strings"
	"testing"
)

// Call logs and solutions observed at 9fa9cc8, before the setting existed.
var offGolden = map[string]string{
	"A": strings.Join([]string{
		"C root 1",
		"D root 1",
		"C b >=1",
		"C c >=1",
		"C d >=1",
		"D b 2",
		"D c 2",
		"D d 2",
		"C d 1 ∪ >=3",
		"D d 1",
		"solution: b=2 c=2 d=1 root=1",
		"order: [root b c d]",
	}, "\n"),
	"C": strings.Join([]string{
		"C root 1",
		"D root 1",
		"C s >=1",
		"C t >=1",
		"D s 1",
		"C u >=1",
		"D t 1",
		"D u 1",
		"solution: root=1 s=1 t=1 u=1",
		"order: [root s t u]",
	}, "\n"),
	"D": strings.Join([]string{
		"C root 1",
		"D root 1",
		"C d >=1",
		"C b >=1",
		"D d 2",
		"D b 2",
		"C c >=2",
		"D c 2",
		"C c >=3",
		"C b 1 ∪ >=3",
		"D b 1",
		"solution: b=1 d=2 root=1",
		"order: [root d b]",
	}, "\n"),
	"E": strings.Join([]string{
		"C root 1",
		"D root 1",
		"C a >=1",
		"C b >=1",
		"D a 2",
		"C c >=1",
		"D b 2",
		"C c 1",
		"C d >=1",
		"D c 1",
		"C e >=2",
		"D d 1",
		"C d >=2",
		"C b 1 ∪ >=3",
		"C c >=1",
		"D b 1",
		"D c 1",
		"D e 2",
		"solution: a=2 b=1 c=1 e=2 root=1",
		"order: [root a b c e]",
	}, "\n"),
}

// TestOffMatchesTheBase pins the setting-off provider calls and solutions of the
// spec's fixtures A, C, D and E to what the base solver did.
func TestOffMatchesTheBase(t *testing.T) {
	fixtures := map[string]func() *universe{"A": fixtureA, "C": fixtureC, "D": fixtureD, "E": fixtureE}
	for name, f := range fixtures {
		t.Run(name, func(t *testing.T) {
			got, err := runLog(f(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got != offGolden[name] {
				t.Errorf("call log changed.\ngot:\n%s\nwant:\n%s", got, offGolden[name])
			}
		})
	}
}
