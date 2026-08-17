package main

import (
	"reflect"
	"testing"
)

func TestE27InputTraceDaemonActionsExposeInspector(t *testing.T) {
	typ := reflect.TypeOf(daemonActions{})
	if _, ok := typ.MethodByName("InspectInputTrace"); !ok {
		t.Fatal("daemonActions missing InspectInputTrace")
	}
}
