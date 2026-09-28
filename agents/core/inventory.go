package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/enfec/agentmesh/protocols/gen/go/agent/v1"
)

var pseudoFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "squashfs": true, "overlay": true, "proc": true, "sysfs": true,
	"cgroup": true, "cgroup2": true, "devpts": true, "mqueue": true, "debugfs": true, "tracefs": true,
	"securityfs": true, "pstore": true, "bpf": true, "autofs": true, "fusectl": true, "configfs": true,
	"hugetlbfs": true, "nsfs": true, "ramfs": true, "efivarfs": true, "binfmt_misc": true, "fuse.lxcfs": true,
	"nullfs": true, "devfs": true, "rpc_pipefs": true, "9p": true,
}

func machineIDHash(ctx context.Context) string {
	id, err := host.HostIDWithContext(ctx)
	if err != nil || id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("agentmesh-machine-id:" + strings.ToLower(id)))
	return hex.EncodeToString(sum[:])
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// collectInventory gathers the hardware/OS snapshot. Partial failures leave
// fields empty rather than failing the whole report.
func collectInventory(ctx context.Context) *agentv1.Inventory {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	inv := &agentv1.Inventory{
		Hostname: hostname(), Platform: runtime.GOOS, Arch: runtime.GOARCH, AgentVersion: Version,
		MachineIdHash: machineIDHash(ctx),
	}
	osi := osInfo(ctx)
	inv.OsName, inv.OsVersion, inv.OsBuild = osi.name, osi.version, osi.build
	if hi, err := host.InfoWithContext(ctx); err == nil {
		inv.KernelVersion = hi.KernelVersion
		inv.UptimeS = hi.Uptime
		if hi.BootTime > 0 {
			inv.BootTime = timestamppb.New(time.Unix(int64(hi.BootTime), 0))
		}
	}
	c := &agentv1.Inventory_Cpu{}
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		c.Model = strings.TrimSpace(infos[0].ModelName)
	}
	if n, err := cpu.CountsWithContext(ctx, false); err == nil {
		c.Cores = uint32(n)
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		c.Threads = uint32(n)
	}
	inv.Cpu = c
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		inv.Memory = &agentv1.Inventory_Memory{TotalBytes: vm.Total, UsedBytes: vm.Used}
	}
	if parts, err := disk.PartitionsWithContext(ctx, false); err == nil {
		seen := map[string]bool{}
		for _, p := range parts {
			root := p.Mountpoint == "/" // always report the root fs (overlay inside containers)
			if (pseudoFS[p.Fstype] && !root) || seen[p.Mountpoint] || strings.HasPrefix(p.Mountpoint, "/snap/") {
				continue
			}
			if st, err := os.Stat(p.Mountpoint); err != nil || !st.IsDir() {
				continue // single-file bind mounts such as /etc/hosts
			}
			seen[p.Mountpoint] = true
			u, err := disk.UsageWithContext(ctx, p.Mountpoint)
			if err != nil || u.Total == 0 {
				continue
			}
			inv.Disks = append(inv.Disks, &agentv1.Inventory_Disk{Mount: p.Mountpoint, Fstype: p.Fstype, TotalBytes: u.Total, UsedBytes: u.Used})
		}
	}
	if ifs, err := net.InterfacesWithContext(ctx); err == nil {
		for _, i := range ifs {
			if hasFlag(i.Flags, "loopback") || len(i.Addrs) == 0 {
				continue
			}
			ni := &agentv1.Inventory_NetInterface{Name: i.Name, Mac: i.HardwareAddr}
			for _, a := range i.Addrs {
				ni.Addrs = append(ni.Addrs, a.Addr)
			}
			inv.Network = append(inv.Network, ni)
		}
	}
	return inv
}

func hasFlag(flags []string, f string) bool {
	for _, x := range flags {
		if x == f {
			return true
		}
	}
	return false
}

// collectHeartbeat gathers the cheap per-interval health snapshot.
func collectHeartbeat(ctx context.Context, seq uint64) *agentv1.Heartbeat {
	hb := &agentv1.Heartbeat{Seq: seq}
	if up, err := host.UptimeWithContext(ctx); err == nil {
		hb.UptimeS = up
	}
	if p, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(p) > 0 {
		hb.CpuPercent = p[0]
	}
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		hb.MemUsedBytes, hb.MemTotalBytes = vm.Used, vm.Total
	}
	if runtime.GOOS != "windows" {
		if l, err := load.AvgWithContext(ctx); err == nil {
			hb.Load1 = l.Load1
		}
	}
	return hb
}
