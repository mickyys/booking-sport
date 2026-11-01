package migratelog

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartWritesTaggedLogFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MIGRATE_LOG_DIR", dir)
	t.Setenv("MONGO_URI", "mongodb://user:secret@host:27017/db?authSource=admin")
	t.Setenv("RECURRING_JOB_TOKEN", "super-secret-token")
	t.Setenv("APPLY", "true")

	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	defer devnull.Close()
	origOut := os.Stdout
	stop := start("migrate-spaces", devnull, devnull)
	log.Printf("[SPACES] migrando %d espacios", 6)
	fmt.Println("salida de fmt")
	stop()
	if os.Stdout != origOut {
		t.Fatal("stdout no se restauró")
	}

	files, _ := filepath.Glob(filepath.Join(dir, "migrate-spaces-*.log"))
	if len(files) != 1 {
		t.Fatalf("esperaba 1 archivo de log, hay %d", len(files))
	}
	raw, _ := os.ReadFile(files[0])
	content := string(raw)

	for _, want := range []string{"INICIO comando=migrate-spaces", "APPLY=true", "[SPACES] migrando 6 espacios", "salida de fmt", "FIN comando=migrate-spaces"} {
		if !strings.Contains(content, want) {
			t.Errorf("falta %q en el log:\n%s", want, content)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		if !strings.HasPrefix(line, Tag+" ") {
			t.Errorf("línea sin tag %s: %q", Tag, line)
		}
	}
	for _, secret := range []string{"secret", "super-secret-token", "user:"} {
		if strings.Contains(content, secret) {
			t.Errorf("el log expone %q:\n%s", secret, content)
		}
	}
}

func TestMaskURI(t *testing.T) {
	if got := MaskURI("mongodb://a:b@h:1/x?y=z"); strings.Contains(got, "a:b") || strings.Contains(got, "y=z") {
		t.Errorf("URI sin enmascarar: %s", got)
	}
	if MaskURI("") != "(por defecto)" {
		t.Error("URI vacía")
	}
}
