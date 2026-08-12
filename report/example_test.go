// SPDX-License-Identifier: Apache-2.0 OR MIT

package report_test

import (
	"fmt"
	"testing"

	"github.com/posit-dev/go-pubgrub/report"
	"github.com/posit-dev/go-pubgrub/solver"
	"github.com/posit-dev/go-pubgrub/versionset"
)

// ExampleFromError shows the whole path from a failed solve to prose.
func ExampleFromError() {
	// root needs alpha, alpha 1 needs bravo 2 or later, and only bravo 1 exists.
	published := newUniverse().
		with("root", 1, requires("alpha", versionset.AtLeast(1))).
		with("alpha", 1, requires("bravo", versionset.AtLeast(2))).
		with("bravo", 1)

	_, err := solver.New[string, set]("root", versionset.Exactly(1), published).Solve()

	explanation, unsatisfiable := report.FromError[string, set](err, nil)
	if !unsatisfiable {
		// Not a conflict: the solve could not be carried out at all.
		fmt.Println("solve failed:", err)
		return
	}
	fmt.Println(explanation)

	// Output:
	// Because no version of alpha matches >=2, alpha 1 depends on bravo >=2 and no version of bravo matches >=2, alpha >=1 cannot be used.
	// So, because root 1 depends on alpha >=1, the requirements of root cannot be satisfied.
}

// ExampleLine_Node shows how to reach the packages involved without parsing the prose.
func ExampleLine_Node() {
	published := newUniverse().
		with("root", 1, requires("alpha", versionset.AtLeast(1))).
		with("alpha", 1, requires("bravo", versionset.AtLeast(2))).
		with("bravo", 1)

	_, err := solver.New[string, set]("root", versionset.Exactly(1), published).Solve()
	explanation, _ := report.FromError[string, set](err, nil)

	for _, line := range explanation.Lines {
		fmt.Println(len(line.Node.Packages()), "package(s) in:", line.Text)
	}

	// Output:
	// 1 package(s) in: Because no version of alpha matches >=2, alpha 1 depends on bravo >=2 and no version of bravo matches >=2, alpha >=1 cannot be used.
	// 1 package(s) in: So, because root 1 depends on alpha >=1, the requirements of root cannot be satisfied.
}

// TestTypeInferenceAtRealCallSites is a compile-time check that a caller passing a
// concrete Formatter does not have to spell out both type arguments.
//
// It matters because every real consumer passes a formatter — that is the point of the
// interface — so if inference failed there, the ergonomic cost would land on exactly the
// call sites that are not tests. Nothing here needs to run; it failing to COMPILE is the
// failure.
func TestTypeInferenceAtRealCallSites(t *testing.T) {
	root := mustFail(t, chainUniverse(), "root", 1)

	if got := report.Explain(root, shoutingFormatter{}); got == nil {
		t.Fatal("inference-only call returned nothing")
	}
	if _, ok := report.FromError(error(nil), shoutingFormatter{}); ok {
		t.Fatal("a nil error is not a resolution failure")
	}
}
