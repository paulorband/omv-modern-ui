package omv

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
)

func randUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// shareState is the in-memory configuration behind the mocked share RPCs.
type shareState struct {
	mu      sync.Mutex
	folders []map[string]any
	smb     []map[string]any
	nfs     []map[string]any
	// Applied counts Config.applyChangesBg calls (tests assert on it).
	Applied int
}

// RegisterShareFixtures installs stateful ShareMgmt/SMB/NFS/UserMgmt/Config handlers on a Mock
// and returns the state so tests can inspect it.
func RegisterShareFixtures(m *Mock) *shareState {
	st := &shareState{}
	st.folders = []map[string]any{{
		"uuid": "aaaaaaaa-0000-4000-8000-000000000001", "name": "media",
		"mntentref": "11111111-aaaa-4aaa-8aaa-111111111111", "reldirpath": "media/", "comment": "Filmes e séries",
		"absdirpath": "/srv/dev-disk-by-uuid-1111/media/",
	}}
	st.smb = []map[string]any{{
		"uuid": "bbbbbbbb-0000-4000-8000-000000000001", "sharedfolderref": "aaaaaaaa-0000-4000-8000-000000000001",
		"enable": true, "guest": "no",
	}}

	list := func(rows *[]map[string]any) MockHandler {
		return func(json.RawMessage) (any, error) {
			st.mu.Lock()
			defer st.mu.Unlock()
			return map[string]any{"total": len(*rows), "data": append([]map[string]any{}, *rows...)}, nil
		}
	}
	m.Handle("ShareMgmt.getList", list(&st.folders))
	m.Handle("SMB.getShareList", list(&st.smb))
	m.Handle("NFS.getShareList", list(&st.nfs))

	set := func(rows *[]map[string]any) MockHandler {
		return func(p json.RawMessage) (any, error) {
			var obj map[string]any
			if err := json.Unmarshal(p, &obj); err != nil {
				return nil, err
			}
			st.mu.Lock()
			defer st.mu.Unlock()
			if obj["uuid"] == NewObjectUUID {
				obj["uuid"] = randUUID()
				*rows = append(*rows, obj)
				return obj, nil
			}
			for i, r := range *rows {
				if r["uuid"] == obj["uuid"] {
					(*rows)[i] = obj
					return obj, nil
				}
			}
			return nil, &RPCError{Code: 6000, Message: "Failed to find object"}
		}
	}
	del := func(rows *[]map[string]any) MockHandler {
		return func(p json.RawMessage) (any, error) {
			var in struct{ UUID string }
			var raw map[string]any
			_ = json.Unmarshal(p, &raw)
			in.UUID, _ = raw["uuid"].(string)
			st.mu.Lock()
			defer st.mu.Unlock()
			for i, r := range *rows {
				if r["uuid"] == in.UUID {
					*rows = append((*rows)[:i], (*rows)[i+1:]...)
					return nil, nil
				}
			}
			return nil, &RPCError{Code: 6000, Message: "Failed to find object"}
		}
	}
	m.Handle("ShareMgmt.set", set(&st.folders))
	m.Handle("ShareMgmt.delete", del(&st.folders))
	m.Handle("SMB.setShare", set(&st.smb))
	m.Handle("SMB.deleteShare", del(&st.smb))
	m.Handle("NFS.deleteShare", del(&st.nfs))

	m.Handle("UserMgmt.enumerateUsers", func(json.RawMessage) (any, error) {
		return []map[string]any{
			{"name": "admin", "uid": 1000, "comment": "Administrador"},
			{"name": "paulo", "uid": 1001, "comment": ""},
		}, nil
	})
	m.Handle("Config.applyChangesBg", func(json.RawMessage) (any, error) {
		st.mu.Lock()
		st.Applied++
		st.mu.Unlock()
		return "/tmp/bgstatus-mock", nil
	})
	m.Handle("Exec.isRunning", func(json.RawMessage) (any, error) {
		return map[string]any{"filename": "/tmp/bgstatus-mock", "running": false}, nil
	})
	m.Handle("ShareMgmt.getCandidates", func(json.RawMessage) (any, error) {
		return []map[string]any{
			{"uuid": "11111111-aaaa-4aaa-8aaa-111111111111", "description": "/dev/sda1 [EXT4, 760.00 GiB (38%) used, 1.21 TiB available]"},
			{"uuid": "22222222-bbbb-4bbb-8bbb-222222222222", "description": "/dev/sdb1 [EXT4, 3.61 TiB (93%) used, 300.00 GiB available]"},
		}, nil
	})
	return st
}

// Counts returns how many folders, SMB shares and NFS shares exist (tests).
func (s *shareState) Counts() (folders, smb, nfs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.folders), len(s.smb), len(s.nfs)
}
