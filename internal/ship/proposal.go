package ship

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

func changeFolder(root, change string) (string, error) {
	active := filepath.Join(root, "openspec", "changes", change)
	if info, err := os.Stat(active); err == nil && info.IsDir() {
		return active, nil
	}
	archive := filepath.Join(root, "openspec", "changes", "archive")
	entries, err := os.ReadDir(archive)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), "-"+change) {
			matches = append(matches, filepath.Join(archive, entry.Name()))
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("cannot find proposal for %s", change)
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}
func ProposalRequest(root, change string, target forge.Target, branch, base string, labels []string, identity Identity) (forge.OpenRequest, error) {
	folder, err := changeFolder(root, change)
	if err != nil {
		return forge.OpenRequest{}, err
	}
	data, err := os.ReadFile(filepath.Join(folder, "proposal.md"))
	if err != nil {
		return forge.OpenRequest{}, err
	}
	var sidecar struct {
		Title string `toml:"title"`
	}
	sideData, err := os.ReadFile(filepath.Join(folder, "tuckerbox.toml"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return forge.OpenRequest{}, err
	}
	if _, err := toml.Decode(string(sideData), &sidecar); err != nil {
		return forge.OpenRequest{}, fmt.Errorf("reading proposal title: %w", err)
	}
	title := sidecar.Title
	if title == "" {
		title = strings.ReplaceAll(change, "-", " ")
	}
	body := "Change: `" + change + "`\n\n" + proposalSections(string(data))
	marker, err := identity.Marker()
	if err != nil {
		return forge.OpenRequest{}, err
	}
	rel, err := filepath.Rel(root, filepath.Join(folder, "proposal.md"))
	if err != nil {
		return forge.OpenRequest{}, err
	}
	route := "/blob/"
	limit := 65536
	if target.Kind == forge.GitLab {
		route = "/-/blob/"
		limit = 1048576
	}
	link := "https://" + target.Host + "/" + target.Project + route + url.PathEscape(branch) + "/" + filepath.ToSlash(rel)
	body = limitBody(body, limit-len(marker)-2, link)
	return forge.OpenRequest{Title: title, Body: body, Branch: branch, Base: base, Labels: labels}, nil
}
func proposalSections(proposal string) string {
	proposal = strings.ReplaceAll(proposal, "\r\n", "\n")
	var out strings.Builder
	keep := false
	for _, line := range strings.Split(proposal, "\n") {
		if strings.HasPrefix(line, "## ") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			keep = name == "Why" || name == "What Changes"
		}
		if keep {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return strings.TrimSpace(out.String())
}
func limitBody(body string, limit int, link string) string {
	if len(body) <= limit {
		return body
	}
	suffix := "\n\n[Full proposal](" + link + ")"
	budget := limit - len(suffix)
	if budget < 0 {
		return suffix
	}
	body = body[:budget]
	for !utf8.ValidString(body) {
		body = body[:len(body)-1]
	}
	return strings.TrimSpace(body) + suffix
}
