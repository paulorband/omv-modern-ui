package omv

import (
	"context"
	"encoding/json"
	"testing"
)

// Trimmed from a real `omv-rpc FileSystemMgmt enumerateFilesystems` on an OMV box (Raspberry Pi):
// "used" is human text ("1.67 TiB"), "size"/"available" are byte strings, swap has "-1".
const realFilesystems = `[
 {"devicefile":"/dev/loop0","uuid":"","label":"origin:rpi-swap","type":"swap","mounted":false,"mountpoint":"","used":"-1","available":"-1","size":"-1","percentage":-1},
 {"devicefile":"/dev/disk/by-uuid/21724cc6-e5a3-48a1-8643-7917dba3a9fb","uuid":"21724cc6-e5a3-48a1-8643-7917dba3a9fb","label":"rootfs","type":"ext4","mounted":true,"mountpoint":"/","used":"24.17 GiB","available":"91648622592","size":"122627121152","percentage":23},
 {"devicefile":"/dev/disk/by-uuid/247A088F7A085FC2","uuid":"247A088F7A085FC2","label":"PRB_Portable","type":"ntfs","mounted":true,"mountpoint":"/srv/dev-disk-by-uuid-247A088F7A085FC2","used":"1.67 TiB","available":"153890979840","size":"2000364236800","percentage":93}
]`

func TestListFilesystemsParsesRealOMVOutput(t *testing.T) {
	call := func(_ context.Context, _, _ string, _, out any) error {
		return json.Unmarshal([]byte(realFilesystems), out)
	}
	fss, err := ListFilesystems(context.Background(), call)
	if err != nil {
		t.Fatalf("real output must parse: %v", err)
	}
	if len(fss) != 2 { // the unmounted swap device is skipped
		t.Fatalf("%+v", fss)
	}
	ntfs := fss[1]
	if ntfs.UUID != "247A088F7A085FC2" || ntfs.Total != 2000364236800 || ntfs.Free != 153890979840 || ntfs.Used != 2000364236800-153890979840 || ntfs.Percent != 92.3 {
		t.Fatalf("%+v", ntfs)
	}
}

// Real `ShareMgmt getCandidates` output, plus the canonical devices of the filesystems above.
const realCandidates = `[{"uuid":"79684322-3eac-11ea-a974-63a080abab18","description":"/dev/mmcblk0p2 [EXT4, 24.21 GiB (23%) used, 85.31 GiB available]"},{"uuid":"72f21b8f-7749-439e-94a8-ae67d0d13f8a","description":"/dev/sda1 [NTFS, 1.67 TiB (93%) used, 143.32 GiB available]"}]`

func TestMntEntRefJoinOnRealOutput(t *testing.T) {
	fsJSON := `[
 {"devicefile":"/dev/disk/by-uuid/21724cc6","canonicaldevicefile":"/dev/mmcblk0p2","uuid":"21724cc6-e5a3-48a1-8643-7917dba3a9fb","label":"rootfs","type":"ext4","mounted":true,"mountpoint":"/","available":"91648622592","size":"122627121152"},
 {"devicefile":"/dev/disk/by-uuid/247A088F7A085FC2","canonicaldevicefile":"/dev/sda1","uuid":"247A088F7A085FC2","label":"PRB_Portable","type":"ntfs","mounted":true,"mountpoint":"/srv/dev-disk-by-uuid-247A088F7A085FC2","available":"153890979840","size":"2000364236800"},
 {"devicefile":"/dev/disk/by-uuid/EACA-13DA","canonicaldevicefile":"/dev/mmcblk0p1","uuid":"EACA-13DA","label":"bootfs","type":"vfat","mounted":true,"mountpoint":"/boot/firmware","available":"459823104","size":"534763520"}
]`
	call := func(_ context.Context, service, method string, _, out any) error {
		switch service + "." + method {
		case "FileSystemMgmt.enumerateFilesystems":
			return json.Unmarshal([]byte(fsJSON), out)
		case "ShareMgmt.getCandidates":
			return json.Unmarshal([]byte(realCandidates), out)
		}
		t.Fatalf("unexpected call %s.%s", service, method)
		return nil
	}
	fss, err := ListFilesystemsWithRefs(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"rootfs":       "79684322-3eac-11ea-a974-63a080abab18",
		"PRB_Portable": "72f21b8f-7749-439e-94a8-ae67d0d13f8a",
		"bootfs":       "", // not offered by OMV as a share location
	}
	for _, f := range fss {
		if f.MntEntRef != want[f.Label] {
			t.Errorf("%s: mntentref %q, want %q", f.Label, f.MntEntRef, want[f.Label])
		}
	}
}

func TestListFilesystemsWithRefsSurvivesMissingCandidates(t *testing.T) {
	call := func(_ context.Context, service, _ string, _, out any) error {
		if service == "ShareMgmt" {
			return &RPCError{Code: 1, Message: "nope"}
		}
		return json.Unmarshal([]byte(realFilesystems), out)
	}
	fss, err := ListFilesystemsWithRefs(context.Background(), call)
	if err != nil || len(fss) != 2 {
		t.Fatalf("%v %+v", err, fss)
	}
}
