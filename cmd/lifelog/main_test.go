package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupCommand(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantBackup bool
		wantError  bool
	}{
		{name: "server", args: nil},
		{name: "backup", args: []string{"backup"}, wantBackup: true},
		{name: "unknown", args: []string{"unknown"}, wantError: true},
		{name: "extra argument", args: []string{"backup", "extra"}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := backupCommand(test.args)
			if got != test.wantBackup || (err != nil) != test.wantError {
				t.Fatalf("backupCommand(%q) = %v, %v", test.args, got, err)
			}
		})
	}
}

func TestCreateServerBackup(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	name, err := createServerBackup(t.Context(), dataDir, backupDir)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(backupDir, name)
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer archive.Close()
	want := map[string]bool{
		"lifelog-backup/journal.db":       false,
		"lifelog-backup/photos/":          false,
		"lifelog-backup/backup-info.json": false,
	}
	for _, file := range archive.File {
		if _, ok := want[file.Name]; ok {
			want[file.Name] = true
		}
	}
	for entry, found := range want {
		if !found {
			t.Errorf("backup is missing %q", entry)
		}
	}
}
