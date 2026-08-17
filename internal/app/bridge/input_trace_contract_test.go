package bridge

import (
	"reflect"
	"testing"
)

func TestE27InputTraceBridgeCarriesDeepInspectionRequestAndResponse(t *testing.T) {
	request := reflect.TypeOf(Request{})
	if _, ok := request.FieldByName("InputTraceInspect"); !ok {
		t.Fatal("bridge Request missing InputTraceInspect")
	}
	response := reflect.TypeOf(Response{})
	if _, ok := response.FieldByName("InputTrace"); !ok {
		t.Fatal("bridge Response missing InputTrace")
	}
}
