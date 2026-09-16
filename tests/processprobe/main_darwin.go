package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/hellices/treeclear/internal/domain"
	"github.com/hellices/treeclear/internal/process"
)

type probeRequest struct {
	Version         int       `json:"version"`
	Challenge       string    `json:"challenge"`
	CallerUID       uint32    `json:"caller_uid"`
	Roots           []string  `json:"roots"`
	ActiveRoot      string    `json:"active_root"`
	ActivePID       int32     `json:"active_pid"`
	ActiveCreatedAt time.Time `json:"active_created_at"`
}

type probeReport struct {
	Version              int            `json:"version"`
	Challenge            string         `json:"challenge"`
	Platform             string         `json:"platform"`
	Architecture         string         `json:"architecture"`
	EffectiveUID         int            `json:"effective_uid"`
	RootCount            int            `json:"root_count"`
	RootsMatched         bool           `json:"roots_matched"`
	EnumerationComplete  bool           `json:"enumeration_complete"`
	ErrorCount           int            `json:"error_count"`
	RetainedErrorCount   int            `json:"retained_error_count"`
	UninspectableCount   int            `json:"uninspectable_count"`
	GlobalUnknownCount   int            `json:"global_unknown_count"`
	ScopedUnknownCount   int            `json:"scoped_unknown_count"`
	DeniedErrorCount     int            `json:"denied_error_count"`
	MissingPathCount     int            `json:"missing_path_count"`
	OtherErrorCount      int            `json:"other_error_count"`
	InvalidArgumentCount int            `json:"invalid_argument_count"`
	NativeReadErrorCount int            `json:"native_read_error_count"`
	ActiveMatched        bool           `json:"active_matched"`
	Complete             bool           `json:"complete"`
	FirstFailureStages   map[string]int `json:"first_failure_stages"`
	NativePathErrnos     map[string]int `json:"native_path_errnos"`
}

type probeCollect func(context.Context, []domain.Worktree) (process.Collection, []error)

func main() {
	watchdog := time.AfterFunc(30*time.Second, func() { os.Exit(124) })
	status := 2
	if len(os.Args) == 1 {
		collector := process.Collector{Source: process.NativeSource()}
		status = runProbe(context.Background(), os.Stdin, os.Stdout, os.Geteuid(), os.Getenv("SUDO_UID"), collector.Collect)
	}
	watchdog.Stop()
	if status == 2 {
		fmt.Fprintln(os.Stderr, "process probe refused or failed protocol validation")
	}
	os.Exit(status)
}

func runProbe(ctx context.Context, input io.Reader, output io.Writer, effectiveUID int, originalUID string, collect probeCollect) int {
	if effectiveUID != 0 || ctx.Err() != nil || collect == nil {
		return 2
	}
	callerUID, err := strconv.ParseUint(originalUID, 10, 32)
	if err != nil || callerUID == 0 || callerUID == uint64(^uint32(0)) || strconv.FormatUint(callerUID, 10) != originalUID {
		return 2
	}
	contents, err := io.ReadAll(io.LimitReader(input, 16385))
	if err != nil || len(contents) > 16384 || ctx.Err() != nil {
		return 2
	}
	var request probeRequest
	if err := json.Unmarshal(contents, &request); err != nil {
		return 2
	}
	canonical, err := json.Marshal(request)
	if err != nil || !bytes.Equal(contents, canonical) || !validRequest(request, uint32(callerUID)) {
		return 2
	}
	worktrees := make([]domain.Worktree, len(request.Roots))
	for index, root := range request.Roots {
		worktrees[index].Path = root
	}
	collectionContext, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	collection, failures := collect(collectionContext, worktrees)
	if err := collectionContext.Err(); err != nil {
		failures = append(failures, err)
	}
	report := summarizeCollection(request, effectiveUID, collection, failures)
	encoded, err := json.Marshal(report)
	if err != nil || len(encoded) >= 16384 {
		return 2
	}
	encoded = append(encoded, '\n')
	if written, err := output.Write(encoded); err != nil || written != len(encoded) {
		return 2
	}
	if !report.Complete {
		return 1
	}
	return 0
}

func validRequest(request probeRequest, callerUID uint32) bool {
	challenge, err := hex.DecodeString(request.Challenge)
	if request.Version != 1 || err != nil || len(challenge) != 32 || hex.EncodeToString(challenge) != request.Challenge || request.CallerUID != callerUID {
		return false
	}
	if len(request.Roots) == 0 || len(request.Roots) > 8 || request.ActivePID <= 0 || !request.ActiveCreatedAt.After(time.Unix(0, 0)) || request.ActiveCreatedAt.Nanosecond()%1000 != 0 {
		return false
	}
	roots := make(map[string]bool, len(request.Roots))
	for _, root := range request.Roots {
		if len(root) > 4096 || strings.ContainsRune(root, 0) || !filepath.IsAbs(root) || filepath.Clean(root) != root || roots[root] {
			return false
		}
		roots[root] = true
	}
	return roots[request.ActiveRoot]
}

func summarizeCollection(request probeRequest, effectiveUID int, collection process.Collection, failures []error) probeReport {
	report := probeReport{
		Version: 1, Challenge: request.Challenge, EffectiveUID: effectiveUID,
		Platform: runtime.GOOS, Architecture: runtime.GOARCH,
		RootCount: len(collection.ByWorktree), RootsMatched: len(collection.ByWorktree) == len(request.Roots),
		EnumerationComplete: collection.Complete, ErrorCount: len(failures),
		RetainedErrorCount: len(collection.Errors), UninspectableCount: len(collection.Uninspectable),
		GlobalUnknownCount: len(collection.GlobalUnknown),
		FirstFailureStages: make(map[string]int),
		NativePathErrnos:   make(map[string]int),
	}
	for _, root := range request.Roots {
		if _, exists := collection.ByWorktree[root]; !exists {
			report.RootsMatched = false
		}
	}
	for _, entries := range collection.ByWorktree {
		for _, evidence := range entries {
			if evidence.State != domain.EvidenceActive || evidence.Error != "" {
				report.ScopedUnknownCount++
			}
		}
	}
	activeMatches := 0
	for _, evidence := range collection.ByWorktree[request.ActiveRoot] {
		if evidence.PID == request.ActivePID && evidence.CreatedAt.Equal(request.ActiveCreatedAt) && evidence.State == domain.EvidenceActive && evidence.Error == "" && evidence.CWD == request.ActiveRoot {
			activeMatches++
		}
	}
	report.ActiveMatched = activeMatches == 1
	for _, failure := range failures {
		message := ""
		if failure != nil {
			message = strings.ToLower(failure.Error())
		}
		report.FirstFailureStages[firstFailureStage(message)]++
		if code := nativePathErrno(message); code != "" {
			report.NativePathErrnos[code]++
		}
		switch {
		case strings.Contains(message, "permission denied"), strings.Contains(message, "operation not permitted"):
			report.DeniedErrorCount++
		case strings.Contains(message, "no such file"):
			report.MissingPathCount++
		case strings.Contains(message, "invalid argument"):
			report.InvalidArgumentCount++
		case strings.Contains(message, "unknown error: proc_pidpath returned"), strings.Contains(message, "unknown error: proc_pidinfo returned"):
			report.NativeReadErrorCount++
		default:
			report.OtherErrorCount++
		}
	}
	report.Complete = report.RootsMatched && report.EnumerationComplete && report.ErrorCount == 0 && report.RetainedErrorCount == 0 && report.UninspectableCount == 0 && report.GlobalUnknownCount == 0 && report.ScopedUnknownCount == 0 && report.ActiveMatched
	return report
}

func firstFailureStage(message string) string {
	if strings.HasPrefix(message, "enumerate processes: ") {
		return "enumeration"
	}
	if strings.HasPrefix(message, "worktree path ") {
		return "worktree-path"
	}
	prefix, remaining, found := strings.Cut(message, ": ")
	if !found || !strings.HasPrefix(prefix, "process ") {
		return "other"
	}
	pidText := strings.TrimPrefix(prefix, "process ")
	pid, err := strconv.ParseInt(pidText, 10, 32)
	if err != nil || pid <= 0 || strconv.FormatInt(pid, 10) != pidText {
		return "other"
	}
	for _, field := range []struct{ prefix, stage string }{
		{"creation time", "creation-time"}, {"executable", "executable"},
		{"cwd", "cwd"}, {"command line", "command-line"},
		{"owner", "owner"}, {"current owner", "owner"}, {"name", "name"},
		{"inspection", "inspection"}, {"process inspection", "inspection"},
		{"containment", "containment"},
	} {
		if strings.HasPrefix(remaining, field.prefix+": ") || strings.HasPrefix(remaining, field.prefix+" ") {
			return field.stage
		}
	}
	return "other"
}

func nativePathErrno(message string) string {
	if firstFailureStage(message) != "executable" {
		return ""
	}
	_, remaining, _ := strings.Cut(message, ": ")
	remaining, found := strings.CutPrefix(remaining, "executable: proc_pidpath errno ")
	if !found {
		return ""
	}
	code, _, _ := strings.Cut(remaining, ": ")
	switch code {
	case "0":
		return "unavailable"
	case "1":
		return "EPERM"
	case "2":
		return "ENOENT"
	case "3":
		return "ESRCH"
	case "5":
		return "EIO"
	case "9":
		return "EBADF"
	case "12":
		return "ENOMEM"
	case "13":
		return "EACCES"
	case "16":
		return "EBUSY"
	case "20":
		return "ENOTDIR"
	case "22":
		return "EINVAL"
	case "35":
		return "EAGAIN"
	case "45":
		return "ENOTSUP"
	case "63":
		return "ENAMETOOLONG"
	case "84":
		return "EOVERFLOW"
	default:
		return "other"
	}
}
