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
