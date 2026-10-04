package storage

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"omvhome/internal/omv"
)

const fsUUID = "11111111-aaaa-4aaa-8aaa-111111111111"

type rig struct {
	m     *omv.Mock
	state interface{ Counts() (int, int, int) }
	call  omv.Caller
	mu    sync.Mutex
	calls []string
	delP  []map[string]any // params of ShareMgmt.delete
}

func newRig(t *testing.T) *rig {
	t.Helper()
	omv.ApplyPollInterval = 5 * time.Millisecond
	m := omv.NewMock()
	omv.RegisterStorageFixtures(m)
	st := omv.RegisterShareFixtures(m)
	sess, err := m.Login(context.Background(), "admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{m: m, state: st}
	r.call = func(ctx context.Context, service, method string, params, out any) error {
		r.mu.Lock()
		r.calls = append(r.calls, service+"."+method)
		if service+"."+method == "ShareMgmt.delete" {
			b, _ := json.Marshal(params)
			var p map[string]any
			_ = json.Unmarshal(b, &p)
			r.delP = append(r.delP, p)
		}
		r.mu.Unlock()
		return m.Call(ctx, sess, service, method, params, out)
	}
	return r
}

// mutating returns the recorded calls that change configuration, in order.
func (r *rig) mutating() []string {
	var out []string
	for _, c := range r.calls {
		switch c {
		case "ShareMgmt.set", "ShareMgmt.delete", "SMB.setShare", "SMB.deleteShare", "NFS.deleteShare", "Config.applyChangesBg":
			out = append(out, c)
		}
	}
	return out
}

func TestListJoinsExportsAndResolvesPath(t *testing.T) {
	r := newRig(t)
	list, err := (&Service{}).List(context.Background(), r.call)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("%+v", list)
	}
	s := list[0]
	if s.Name != "media" || !s.SMB || s.SMBGuest || s.NFS || s.Path != "/srv/dev-disk-by-uuid-1111/media" || s.FilesystemUUID != fsUUID {
		t.Errorf("%+v", s)
	}
}

func TestListToleratesMissingSMBAndNFS(t *testing.T) {
	r := newRig(t)
	boom := func(json.RawMessage) (any, error) {
		return nil, &omv.RPCError{Code: 1, Message: "service not available"}
	}
	r.m.Handle("SMB.getShareList", boom)
	r.m.Handle("NFS.getShareList", boom)
	list, err := (&Service{}).List(context.Background(), r.call)
	if err != nil || len(list) != 1 || list[0].SMB {
		t.Fatalf("%v %+v", err, list)
	}
}

func TestCreateChainsCallsInOrder(t *testing.T) { // A06.1
	r := newRig(t)
	sh, err := (&Service{}).Create(context.Background(), r.call, CreateInput{Name: "docs", FilesystemUUID: fsUUID, SMB: true, SMBGuest: true, Comment: "Documentos"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ShareMgmt.set", "SMB.setShare", "Config.applyChangesBg"}
	if got := r.mutating(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls: %v want %v", got, want)
	}
	if sh.Name != "docs" || !sh.SMB || !sh.SMBGuest || sh.Path != "/srv/dev-disk-by-uuid-1111/docs" {
		t.Errorf("%+v", sh)
	}
}

func TestCreateWithoutSMBSkipsSMBStep(t *testing.T) {
	r := newRig(t)
	if _, err := (&Service{}).Create(context.Background(), r.call, CreateInput{Name: "plain", FilesystemUUID: fsUUID, RelativePath: "data/plain"}); err != nil {
		t.Fatal(err)
	}
	if got := r.mutating(); !reflect.DeepEqual(got, []string{"ShareMgmt.set", "Config.applyChangesBg"}) {
		t.Fatalf("%v", got)
	}
}

func TestCreateRollsBackWhenSMBFails(t *testing.T) { // A06.2
	r := newRig(t)
	r.m.Handle("SMB.setShare", func(json.RawMessage) (any, error) { return nil, &omv.RPCError{Code: 1, Message: "smb broken"} })
	_, err := (&Service{}).Create(context.Background(), r.call, CreateInput{Name: "docs", FilesystemUUID: fsUUID, SMB: true})
	var se *StepError
	if !errors.As(err, &se) || !strings.Contains(se.Step, "SMB") {
		t.Fatalf("want StepError for SMB, got %v", err)
	}
	want := []string{"ShareMgmt.set", "SMB.setShare", "ShareMgmt.delete"}
	if got := r.mutating(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls: %v want %v", got, want)
	}
	if folders, _, _ := r.state.Counts(); folders != 1 { // only the seeded one remains
		t.Fatalf("folder not rolled back: %d", folders)
	}
}

func TestCreateRollsBackWhenApplyFails(t *testing.T) {
	r := newRig(t)
	r.m.Handle("Config.applyChangesBg", func(json.RawMessage) (any, error) { return nil, &omv.RPCError{Code: 1, Message: "salt failed"} })
	_, err := (&Service{}).Create(context.Background(), r.call, CreateInput{Name: "docs", FilesystemUUID: fsUUID, SMB: true})
	var se *StepError
	if !errors.As(err, &se) || se.Step != "aplicar alterações" {
		t.Fatalf("%v", err)
	}
	folders, smb, _ := r.state.Counts()
	if folders != 1 || smb != 1 {
		t.Fatalf("rollback incomplete: folders=%d smb=%d", folders, smb)
	}
}

func TestCreateValidationDoesNotCallOMV(t *testing.T) { // A06.3
	r := newRig(t)
	bad := []CreateInput{
		{Name: "", FilesystemUUID: fsUUID},
		{Name: "has space", FilesystemUUID: fsUUID},
		{Name: strings.Repeat("a", 33), FilesystemUUID: fsUUID},
		{Name: "ok", FilesystemUUID: "not-a-uuid"},
		{Name: "ok", FilesystemUUID: fsUUID, RelativePath: "../etc"},
		{Name: "ok", FilesystemUUID: fsUUID, RelativePath: "/abs"},
		{Name: "ok", FilesystemUUID: fsUUID, RelativePath: "a/../../b"},
		{Name: "ok", FilesystemUUID: fsUUID, RelativePath: "bad;rm"},
		{Name: "ok", FilesystemUUID: fsUUID, Comment: "line\nbreak"},
		{Name: "ok", FilesystemUUID: fsUUID, SMBGuest: true}, // guest without SMB
	}
	for _, in := range bad {
		_, err := (&Service{}).Create(context.Background(), r.call, in)
		var ie *InvalidError
		if !errors.As(err, &ie) {
			t.Errorf("%+v: want InvalidError, got %v", in, err)
		}
	}
	if len(r.calls) != 0 {
		t.Fatalf("OMV must not be called: %v", r.calls)
	}
}

func TestCreateDuplicateNameIsConflict(t *testing.T) {
	r := newRig(t)
	_, err := (&Service{}).Create(context.Background(), r.call, CreateInput{Name: "MEDIA", FilesystemUUID: fsUUID})
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("got %v", err)
	}
	if len(r.mutating()) != 0 {
		t.Fatalf("no mutation expected: %v", r.mutating())
	}
}

func TestDeleteRemovesExportsAndKeepsData(t *testing.T) { // A06.4
	r := newRig(t)
	svc := &Service{}
	if err := svc.Delete(context.Background(), r.call, "aaaaaaaa-0000-4000-8000-000000000001"); err != nil {
		t.Fatal(err)
	}
	want := []string{"SMB.deleteShare", "ShareMgmt.delete", "Config.applyChangesBg"}
	if got := r.mutating(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls: %v want %v", got, want)
	}
	if len(r.delP) != 1 || r.delP[0]["recursive"] != false {
		t.Fatalf("ShareMgmt.delete must pass recursive=false: %v", r.delP)
	}
	if err := svc.Delete(context.Background(), r.call, "aaaaaaaa-0000-4000-8000-000000000001"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if err := svc.Delete(context.Background(), r.call, "../x"); err == nil {
		t.Fatal("invalid uuid must fail")
	}
}

func TestUpdateTogglesSMB(t *testing.T) {
	r := newRig(t)
	svc := &Service{}
	id := "aaaaaaaa-0000-4000-8000-000000000001"
	yes, no := true, false

	sh, err := svc.Update(context.Background(), r.call, id, UpdateInput{SMBGuest: &yes})
	if err != nil || !sh.SMB || !sh.SMBGuest {
		t.Fatalf("enable guest: %v %+v", err, sh)
	}
	sh, err = svc.Update(context.Background(), r.call, id, UpdateInput{SMB: &no})
	if err != nil || sh.SMB || sh.SMBGuest {
		t.Fatalf("disable smb: %v %+v", err, sh)
	}
	sh, err = svc.Update(context.Background(), r.call, id, UpdateInput{SMB: &yes})
	if err != nil || !sh.SMB {
		t.Fatalf("re-enable smb: %v %+v", err, sh)
	}
	if _, smb, _ := r.state.Counts(); smb != 1 {
		t.Fatalf("smb exports: %d", smb)
	}
	before := len(r.mutating())
	if _, err := svc.Update(context.Background(), r.call, id, UpdateInput{SMB: &yes}); err != nil || len(r.mutating()) != before {
		t.Fatalf("no-op update must not touch OMV: %v", err)
	}
	if _, err := svc.Update(context.Background(), r.call, id, UpdateInput{SMB: &no, SMBGuest: &yes}); err == nil {
		t.Fatal("guest without smb must fail")
	}
}

func TestApplyChangesPollsUntilDone(t *testing.T) {
	r := newRig(t)
	polls := 0
	r.m.Handle("Exec.isRunning", func(json.RawMessage) (any, error) { polls++; return polls < 3, nil })
	if err := omv.ApplyChanges(context.Background(), r.call); err != nil {
		t.Fatal(err)
	}
	if polls != 3 {
		t.Fatalf("polls=%d", polls)
	}
}

func TestSerialisedMutations(t *testing.T) {
	r := newRig(t)
	svc := &Service{}
	var wg sync.WaitGroup
	for _, n := range []string{"one", "two", "three", "four"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Create(context.Background(), r.call, CreateInput{Name: n, FilesystemUUID: fsUUID}); err != nil {
				t.Errorf("%s: %v", n, err)
			}
		}()
	}
	wg.Wait()
	if folders, _, _ := r.state.Counts(); folders != 5 {
		t.Fatalf("folders=%d", folders)
	}
}
