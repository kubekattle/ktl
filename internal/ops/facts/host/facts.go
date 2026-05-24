package hostfacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const (
	APIVersion = "torque.dev/ops/facts/v1alpha1"
	Kind       = "HostFactSnapshot"
)

type Transport interface {
	TargetDigest() string
	Run(ctx context.Context, command string) transport.OperationResult
}

type CollectRequest struct {
	TargetID   string
	ObservedAt time.Time
	TTL        time.Duration
}

type Snapshot struct {
	APIVersion      string                      `json:"apiVersion"`
	Kind            string                      `json:"kind"`
	TargetID        string                      `json:"targetId"`
	TargetDigest    string                      `json:"targetDigest"`
	ObservedAt      string                      `json:"observedAt"`
	TTL             string                      `json:"ttl"`
	ExpiresAt       string                      `json:"expiresAt"`
	Digest          string                      `json:"digest"`
	OS              OSFacts                     `json:"os"`
	Kernel          KernelFacts                 `json:"kernel"`
	Packages        PackageFacts                `json:"packages"`
	Services        ServiceFacts                `json:"services"`
	Users           UserFacts                   `json:"users"`
	Disks           DiskFacts                   `json:"disks"`
	Network         NetworkFacts                `json:"network"`
	CommandReceipts []transport.OperationResult `json:"commandReceipts"`
}

type OSFacts struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
	VersionID  string `json:"versionId,omitempty"`
	PrettyName string `json:"prettyName,omitempty"`
}

type KernelFacts struct {
	Name    string `json:"name"`
	Release string `json:"release"`
	Machine string `json:"machine"`
}

type PackageFacts struct {
	Manager string   `json:"manager"`
	Count   int      `json:"count"`
	Sample  []string `json:"sample,omitempty"`
}

type ServiceFacts struct {
	Manager      string   `json:"manager"`
	Count        int      `json:"count"`
	RunningCount int      `json:"runningCount"`
	Sample       []string `json:"sample,omitempty"`
}

type UserFacts struct {
	Count           int      `json:"count"`
	LoginShellCount int      `json:"loginShellCount"`
	Sample          []string `json:"sample,omitempty"`
}

type DiskFacts struct {
	Count       int        `json:"count"`
	Filesystems []DiskInfo `json:"filesystems,omitempty"`
}

type DiskInfo struct {
	Filesystem  string `json:"filesystem"`
	Type        string `json:"type,omitempty"`
	SizeKB      int64  `json:"sizeKb,omitempty"`
	UsedKB      int64  `json:"usedKb,omitempty"`
	AvailableKB int64  `json:"availableKb,omitempty"`
	Capacity    string `json:"capacity,omitempty"`
	Mountpoint  string `json:"mountpoint"`
}

type NetworkFacts struct {
	Hostname       string        `json:"hostname"`
	InterfaceCount int           `json:"interfaceCount"`
	AddressCount   int           `json:"addressCount"`
	Addresses      []AddressInfo `json:"addresses,omitempty"`
}

type AddressInfo struct {
	Interface string `json:"interface"`
	Family    string `json:"family"`
	Address   string `json:"address"`
}

func Collect(ctx context.Context, target Transport, request CollectRequest) (*Snapshot, error) {
	if target == nil {
		return nil, fmt.Errorf("transport is required")
	}
	targetID := strings.TrimSpace(request.TargetID)
	if targetID == "" {
		return nil, fmt.Errorf("target id is required")
	}
	ttl := request.TTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	observedAt := request.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	results := map[string]transport.OperationResult{}
	for _, command := range factCommands() {
		result := target.Run(ctx, command.command)
		result.Operation = command.name
		results[command.name] = result
		if result.Status != "succeeded" {
			return nil, fmt.Errorf("%s failed: %s", command.name, result.Status)
		}
	}

	snapshot := &Snapshot{
		APIVersion:   APIVersion,
		Kind:         Kind,
		TargetID:     targetID,
		TargetDigest: target.TargetDigest(),
		ObservedAt:   observedAt.Format(time.RFC3339),
		TTL:          ttl.String(),
		ExpiresAt:    observedAt.Add(ttl).Format(time.RFC3339),
		OS:           parseOSRelease(results["fact.osRelease"].Stdout),
		Kernel:       parseKernel(results["fact.kernel"].Stdout),
		Packages:     parsePackages(results["fact.packages"].Stdout),
		Services:     parseServices(results["fact.services"].Stdout),
		Users:        parseUsers(results["fact.users"].Stdout),
		Disks:        parseDisks(results["fact.disks"].Stdout),
		Network:      parseNetwork(results["fact.network"].Stdout),
		CommandReceipts: []transport.OperationResult{
			results["fact.osRelease"],
			results["fact.kernel"],
			results["fact.packages"],
			results["fact.services"],
			results["fact.users"],
			results["fact.disks"],
			results["fact.network"],
		},
	}
	snapshot.Digest = snapshot.StableDigest()
	return snapshot, nil
}

func (s Snapshot) StableDigest() string {
	doc := struct {
		TargetID     string       `json:"targetId"`
		TargetDigest string       `json:"targetDigest"`
		OS           OSFacts      `json:"os"`
		Kernel       KernelFacts  `json:"kernel"`
		Packages     PackageFacts `json:"packages"`
		Services     ServiceFacts `json:"services"`
		Users        UserFacts    `json:"users"`
		Disks        DiskFacts    `json:"disks"`
		Network      NetworkFacts `json:"network"`
	}{
		TargetID:     s.TargetID,
		TargetDigest: s.TargetDigest,
		OS:           s.OS,
		Kernel:       s.Kernel,
		Packages:     s.Packages,
		Services:     s.Services,
		Users:        s.Users,
		Disks:        s.Disks,
		Network:      s.Network,
	}
	raw, _ := json.Marshal(doc)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type factCommand struct {
	name    string
	command string
}

func factCommands() []factCommand {
	return []factCommand{
		{name: "fact.osRelease", command: `if [ -r /etc/os-release ]; then cat /etc/os-release; else printf 'ID=unknown\nNAME=unknown\n'; fi`},
		{name: "fact.kernel", command: `uname -s; uname -r; uname -m`},
		{name: "fact.packages", command: packageCommand},
		{name: "fact.services", command: serviceCommand},
		{name: "fact.users", command: userCommand},
		{name: "fact.disks", command: diskCommand},
		{name: "fact.network", command: networkCommand},
	}
}

const packageCommand = `if command -v dpkg-query >/dev/null 2>&1; then
  echo manager=dpkg
  dpkg-query -W -f='${binary:Package}\n' 2>/dev/null | sort | awk 'BEGIN{c=0} {c++; if(c<=50) print "package="$0} END{print "count="c+0}'
elif command -v rpm >/dev/null 2>&1; then
  echo manager=rpm
  rpm -qa 2>/dev/null | sort | awk 'BEGIN{c=0} {c++; if(c<=50) print "package="$0} END{print "count="c+0}'
elif command -v apk >/dev/null 2>&1; then
  echo manager=apk
  apk info 2>/dev/null | sort | awk 'BEGIN{c=0} {c++; if(c<=50) print "package="$0} END{print "count="c+0}'
else
  echo manager=unknown
  echo count=0
fi`

const serviceCommand = `if command -v systemctl >/dev/null 2>&1; then
  echo manager=systemd
  systemctl list-units --type=service --all --no-pager --no-legend 2>/dev/null | awk 'BEGIN{c=0;r=0} NF{c++; if($3=="running") r++; if(c<=50) print "service="$1","$3} END{print "count="c+0; print "running="r+0}'
elif [ -d /etc/init.d ]; then
  echo manager=sysvinit
  find /etc/init.d -maxdepth 1 -type f 2>/dev/null | sort | awk -F/ 'BEGIN{c=0} NF{c++; if(c<=50) print "service="$NF",unknown"} END{print "count="c+0; print "running=0"}'
else
  echo manager=unknown
  echo count=0
  echo running=0
fi`

const userCommand = `if command -v getent >/dev/null 2>&1; then getent passwd; elif [ -r /etc/passwd ]; then cat /etc/passwd; fi | awk -F: 'BEGIN{c=0;l=0} NF>=7{c++; if($7 !~ /(nologin|false)$/) l++; if(c<=50) print "user="$1","$7} END{print "count="c+0; print "loginShells="l+0}'`

const diskCommand = `if df -P -T >/dev/null 2>&1; then df -P -T; else df -P; fi`

const networkCommand = `printf 'hostname='; hostname 2>/dev/null || echo unknown
if command -v ip >/dev/null 2>&1; then
  ip -o addr show 2>/dev/null | awk 'NF>=4{print "addr="$2","$3","$4}'
elif command -v hostname >/dev/null 2>&1; then
  hostname -I 2>/dev/null | tr ' ' '\n' | awk 'NF{print "addr=unknown,inet,"$1}'
fi`

func parseOSRelease(raw string) OSFacts {
	values := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		values[parts[0]] = strings.Trim(strings.TrimSpace(parts[1]), `"`)
	}
	return OSFacts{
		ID:         values["ID"],
		Name:       values["NAME"],
		VersionID:  values["VERSION_ID"],
		PrettyName: values["PRETTY_NAME"],
	}
}

func parseKernel(raw string) KernelFacts {
	lines := nonEmptyLines(raw)
	facts := KernelFacts{}
	if len(lines) > 0 {
		facts.Name = lines[0]
	}
	if len(lines) > 1 {
		facts.Release = lines[1]
	}
	if len(lines) > 2 {
		facts.Machine = lines[2]
	}
	return facts
}

func parsePackages(raw string) PackageFacts {
	facts := PackageFacts{Manager: "unknown"}
	for _, line := range nonEmptyLines(raw) {
		key, value, ok := splitKV(line)
		if !ok {
			continue
		}
		switch key {
		case "manager":
			facts.Manager = value
		case "count":
			facts.Count = atoi(value)
		case "package":
			facts.Sample = append(facts.Sample, value)
		}
	}
	return facts
}

func parseServices(raw string) ServiceFacts {
	facts := ServiceFacts{Manager: "unknown"}
	for _, line := range nonEmptyLines(raw) {
		key, value, ok := splitKV(line)
		if !ok {
			continue
		}
		switch key {
		case "manager":
			facts.Manager = value
		case "count":
			facts.Count = atoi(value)
		case "running":
			facts.RunningCount = atoi(value)
		case "service":
			facts.Sample = append(facts.Sample, value)
		}
	}
	return facts
}

func parseUsers(raw string) UserFacts {
	var facts UserFacts
	for _, line := range nonEmptyLines(raw) {
		key, value, ok := splitKV(line)
		if !ok {
			continue
		}
		switch key {
		case "count":
			facts.Count = atoi(value)
		case "loginShells":
			facts.LoginShellCount = atoi(value)
		case "user":
			facts.Sample = append(facts.Sample, value)
		}
	}
	return facts
}

func parseDisks(raw string) DiskFacts {
	var facts DiskFacts
	for i, line := range nonEmptyLines(raw) {
		if i == 0 && strings.HasPrefix(line, "Filesystem") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		info := DiskInfo{Filesystem: fields[0]}
		if len(fields) >= 7 {
			info.Type = fields[1]
			info.SizeKB = atoi64(fields[2])
			info.UsedKB = atoi64(fields[3])
			info.AvailableKB = atoi64(fields[4])
			info.Capacity = fields[5]
			info.Mountpoint = strings.Join(fields[6:], " ")
		} else {
			info.SizeKB = atoi64(fields[1])
			info.UsedKB = atoi64(fields[2])
			info.AvailableKB = atoi64(fields[3])
			info.Capacity = fields[4]
			info.Mountpoint = strings.Join(fields[5:], " ")
		}
		facts.Filesystems = append(facts.Filesystems, info)
	}
	facts.Count = len(facts.Filesystems)
	return facts
}

func parseNetwork(raw string) NetworkFacts {
	facts := NetworkFacts{}
	interfaces := map[string]struct{}{}
	for _, line := range nonEmptyLines(raw) {
		key, value, ok := splitKV(line)
		if !ok {
			continue
		}
		switch key {
		case "hostname":
			facts.Hostname = value
		case "addr":
			parts := strings.SplitN(value, ",", 3)
			if len(parts) != 3 {
				continue
			}
			address := AddressInfo{
				Interface: parts[0],
				Family:    parts[1],
				Address:   parts[2],
			}
			facts.Addresses = append(facts.Addresses, address)
			interfaces[address.Interface] = struct{}{}
		}
	}
	sort.Slice(facts.Addresses, func(i, j int) bool {
		left := facts.Addresses[i]
		right := facts.Addresses[j]
		return left.Interface+","+left.Family+","+left.Address < right.Interface+","+right.Family+","+right.Address
	})
	facts.AddressCount = len(facts.Addresses)
	facts.InterfaceCount = len(interfaces)
	return facts
}

func splitKV(line string) (string, string, bool) {
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), true
}

func nonEmptyLines(raw string) []string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func atoi(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func atoi64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}
