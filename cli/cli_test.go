package cli

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/service/rds"
)

type FakeNower struct {
	t time.Time
}

func (f *FakeNower) Now() time.Time {
	return f.t
}

func TestGetNextMarker(t *testing.T) {
	// next position is legit
	c := CLI{}
	c.fakeNower = &FakeNower{}
	c.fakeNower.(*FakeNower).t, _ = time.Parse(time.RFC3339, "2010-06-21T15:12:05Z")
	startingPos := "12:1234"
	streamPos := StreamPos{
		logFile: LogFile{},
		marker:  &startingPos,
	}
	resp := rds.DownloadDBLogFilePortionOutput{}
	respPos := "12:2345"
	resp.Marker = &respPos
	nextMarker := c.getNextMarker(streamPos, &resp)
	if resp.Marker == nil {
		t.Error("unexpected resp marker nil")
	}
	if *nextMarker != respPos {
		t.Errorf("response marker %s expected, got %s", respPos, *nextMarker)
	}
	// next position unchanged but legit
	respPos = "12:1234"
	resp.Marker = &respPos
	nextMarker = c.getNextMarker(streamPos, &resp)
	if resp.Marker == nil {
		t.Error("unexpected resp marker nil")
	}
	if *nextMarker != respPos {
		t.Errorf("response marker %s expected, got %s", respPos, *nextMarker)
	}
	// next position 0 and no data, not in :00-:05 time range, expect resp
	respPos = "0"
	resp.Marker = &respPos
	nextMarker = c.getNextMarker(streamPos, &resp)
	if resp.Marker == nil {
		t.Error("unexpected resp marker nil")
	}
	if *nextMarker != respPos {
		t.Errorf("response marker %s expected, got %s", respPos, *nextMarker)
	}
	// next position 0 and no data, in :00-:05 time range, expect startingPos
	c.fakeNower.(*FakeNower).t, _ = time.Parse(time.RFC3339, "2010-06-21T15:03:05Z")
	respPos = "0"
	resp.Marker = &respPos
	nextMarker = c.getNextMarker(streamPos, &resp)
	if resp.Marker == nil {
		t.Error("unexpected resp marker nil")
	}
	if *nextMarker != startingPos {
		t.Errorf("response marker %s expected, got %s", startingPos, *nextMarker)
	}
	// next position 0 and have data, not in :00-:05 time range, expect start+len
	respContent := "this is a slow query log entry, really."
	expectedMarker := "12:1273" // 1234 + 39 (aka len(respContent))
	resp.LogFileData = &respContent
	respPos = "0"
	resp.Marker = &respPos
	nextMarker = c.getNextMarker(streamPos, &resp)
	if resp.Marker == nil {
		t.Error("unexpected resp marker nil")
	}
	if *nextMarker != expectedMarker {
		t.Errorf("response marker %s expected, got %s", expectedMarker, *nextMarker)
	}
	// next position 0 and have data, in :00-:05 time range, expect start+len
	c.fakeNower.(*FakeNower).t, _ = time.Parse(time.RFC3339, "2010-06-21T15:03:05Z")
	respPos = "0"
	resp.Marker = &respPos
	nextMarker = c.getNextMarker(streamPos, &resp)
	if resp.Marker == nil {
		t.Error("unexpected resp marker nil")
	}
	if *nextMarker != expectedMarker {
		t.Errorf("response marker %s expected, got %s", expectedMarker, *nextMarker)
	}
}

func TestStreamAdd(t *testing.T) {
	startingPos := "12:1234"
	streamPos := StreamPos{
		marker: &startingPos,
	}
	lenToAdd := 60
	expectedPos := "12:1294"
	sumPos, err := streamPos.Add(lenToAdd)
	if err != nil {
		t.Errorf("unexpected error returned %s", err)
	}
	if sumPos != expectedPos {
		t.Errorf("position %s added to length %d got %s, expected %s", startingPos,
			lenToAdd, sumPos, expectedPos)
	}
}

func TestLogFileEqual(t *testing.T) {
	// base
	lfA := LogFile{
		Size:        45,
		LogFileName: "foo",
		LastWritten: 67,
	}
	// same as A, despite different name
	lfB := LogFile{
		Size:        45,
		LogFileName: "bar",
		LastWritten: 67,
	}
	// diff LastWritten
	lfC := LogFile{
		Size:        45,
		LogFileName: "bar",
		LastWritten: 89,
	}
	// diff Size
	lfD := LogFile{
		Size:        56,
		LogFileName: "bar",
		LastWritten: 67,
	}
	// diff Size and LastWritten
	lfE := LogFile{
		Size:        56,
		LogFileName: "bar",
		LastWritten: 89,
	}
	if !lfA.Equal(lfB) {
		t.Errorf("expected lfA == lfB. (%+v, %+v)", lfA, lfB)
	}
	if lfA.Equal(lfC) {
		t.Errorf("expected lfA != lfC. (%+v, %+v)", lfA, lfC)
	}
	if lfA.Equal(lfD) {
		t.Errorf("expected lfA != lfD. (%+v, %+v)", lfA, lfD)
	}
	if lfA.Equal(lfE) {
		t.Errorf("expected lfA != lfE. (%+v, %+v)", lfA, lfE)
	}

}
