package selfupdate

import (
	"reflect"
	"testing"

	"github.com/brohd11/goutil/selfupdate"

	"github.com/brohd11/bubblestack/components"
)

// Hooks converts between selfupdate.Info and components.SelfUpdateInfo with a
// direct struct cast, which only stays correct while the two structs are
// field-identical. This test makes any drift on either side fail loudly here
// instead of silently garbling version info in every app using the bridge.
func TestInfoStructsStayFieldIdentical(t *testing.T) {
	gt := reflect.TypeOf(selfupdate.Info{})
	ct := reflect.TypeOf(components.SelfUpdateInfo{})

	if gt.NumField() != ct.NumField() {
		t.Fatalf("field count drift: selfupdate.Info has %d fields, components.SelfUpdateInfo has %d",
			gt.NumField(), ct.NumField())
	}
	for i := 0; i < gt.NumField(); i++ {
		g, c := gt.Field(i), ct.Field(i)
		if g.Name != c.Name || g.Type != c.Type {
			t.Errorf("field %d drift: selfupdate.Info has %s %s, components.SelfUpdateInfo has %s %s",
				i, g.Name, g.Type, c.Name, c.Type)
		}
	}
}

// Hooks must wire every field of the flow's hook set; a missing Check/Apply
// would nil-panic inside the shared flow at runtime, not at build time.
func TestHooksReturnsCompleteHookSet(t *testing.T) {
	hooks := Hooks("myapp", "brohd11/myapp", "v1.2.3")

	if hooks.AppName != "myapp" {
		t.Errorf("AppName = %q, want %q", hooks.AppName, "myapp")
	}
	if hooks.Check == nil {
		t.Error("Check is nil")
	}
	if hooks.Apply == nil {
		t.Error("Apply is nil")
	}
}
