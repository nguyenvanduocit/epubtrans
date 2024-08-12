package util

import (
	"path/filepath"
	"testing"
)

func TestGetUnzipDestination(t *testing.T) {
	type args struct {
		zipPath string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name:    "Valid zip path with extension",
			args:    args{zipPath: filepath.Join("path", "to", "file.zip")},
			want:    filepath.Join("path", "to", "file"),
			wantErr: false,
		},
		{
			name:    "Valid zip path without extension",
			args:    args{zipPath: filepath.Join("path", "to", "file")},
			want:    filepath.Join("path", "to", "file"),
			wantErr: false,
		},
		{
			name:    "Valid zip path with multiple extensions",
			args:    args{zipPath: filepath.Join("path", "to", "file.tar.gz")},
			want:    filepath.Join("path", "to", "file.tar"),
			wantErr: false,
		},
		{
			name:    "Empty zip path",
			args:    args{zipPath: ""},
			want:    "",
			wantErr: true,
		},
		{
			name:    "Zip file in root directory",
			args:    args{zipPath: filepath.Join("/", "file.zip")},
			want:    filepath.Join("/", "file"),
			wantErr: false,
		},
		{
			name:    "Zip file with spaces in name",
			args:    args{zipPath: filepath.Join("path", "to", "my file.zip")},
			want:    filepath.Join("path", "to", "my file"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetUnzipDestination(tt.args.zipPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetUnzipDestination() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetUnzipDestination() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetUnzipDestinationWindowsPaths(t *testing.T) {
	tests := []struct {
		name    string
		zipPath string
		want    string
		wantErr bool
	}{
		{
			name:    "Windows path with drive letter",
			zipPath: `C:\Users\username\Documents\file.zip`,
			want:    `C:\Users\username\Documents\file`,
			wantErr: false,
		},
		{
			name:    "Windows UNC path",
			zipPath: `\\server\share\file.zip`,
			want:    `\\server\share\file`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetUnzipDestination(tt.zipPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetUnzipDestination() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetUnzipDestination() = %v, want %v", got, tt.want)
			}
		})
	}
}
