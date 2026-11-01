// Package migratelog deja un archivo de log, con el tag [Migrate], de todo lo
// que hace un comando de migración. Cada ejecución crea su propio archivo
// (migrate-<comando>-<fecha UTC>-<pid>.log) en MIGRATE_LOG_DIR (por defecto
// logs/migrations) y duplica la salida en la consola.
//
// Uso al inicio de main():
//
//	defer migratelog.Start("migrate-spaces")()
//
// Cubre tanto el paquete log (log.Printf/Fatalf) como lo que se imprima en
// stdout (fmt.Printf). Cada línea queda con el prefijo "[Migrate] ".
//
// Si el comando termina con log.Fatal, la última línea del archivo es el error
// y NO hay línea "FIN": la ausencia de "FIN" indica una ejecución fallida.
package migratelog

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Tag es el prefijo de todas las líneas.
const Tag = "[Migrate]"

// envKeys son las variables que se registran al inicio. Es una lista blanca:
// nunca se registran tokens ni secretos.
var envKeys = []string{
	"MONGO_DB", "APPLY", "DRY_RUN", "ROLLBACK", "FORCE", "FORCE_FUTURE", "DROP_ORIGINAL",
	"CENTER_ID", "SPACE_IDS", "NAME_PATTERN", "DEFAULT_COMMISSION", "OVERRIDES", "TIMEZONE",
	"SINCE", "FROM", "TO", "LIMIT", "API_BASE_URL", "STRICT", "BOOKING_CODES", "RECURRING_IDS",
}

// Start abre el archivo de log, redirige log y stdout y devuelve la función
// que cierra el registro (línea FIN, duración y cierre del archivo).
func Start(command string) func() {
	return start(command, os.Stdout, os.Stderr)
}

func start(command string, stdout, stderr *os.File) func() {
	dir := os.Getenv("MIGRATE_LOG_DIR")
	if dir == "" {
		dir = filepath.Join("logs", "migrations")
	}
	began := time.Now().UTC()
	name := fmt.Sprintf("migrate-%s-%s-%d.log", strings.TrimPrefix(command, "migrate-"), began.Format("20060102-150405"), os.Getpid())
	path := filepath.Join(dir, name)

	var file *os.File
	if err := os.MkdirAll(dir, 0o755); err == nil {
		file, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			file = nil
		}
	}
	if file == nil {
		fmt.Fprintf(stderr, "%s ADVERTENCIA: no se pudo crear el archivo de log en %s\n", Tag, dir)
	}

	var mu sync.Mutex
	fileWriter := writerFunc(func(p []byte) (int, error) {
		if file == nil {
			return len(p), nil
		}
		mu.Lock()
		defer mu.Unlock()
		return file.Write(p)
	})

	log.SetPrefix(Tag + " ")
	log.SetOutput(io.MultiWriter(stderr, fileWriter))

	// stdout (fmt.Printf) pasa por un pipe que etiqueta cada línea.
	origStdout := os.Stdout
	pr, pw, err := os.Pipe()
	done := make(chan struct{})
	if err == nil {
		os.Stdout = pw
		go func() {
			defer close(done)
			sc := bufio.NewScanner(pr)
			sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
			for sc.Scan() {
				line := fmt.Sprintf("%s %s %s\n", Tag, time.Now().Format("2006/01/02 15:04:05"), sc.Text())
				_, _ = io.WriteString(stdout, line)
				_, _ = fileWriter.Write([]byte(line))
			}
		}()
	} else {
		close(done)
	}

	log.Printf("INICIO comando=%s pid=%d go=%s host=%s", command, os.Getpid(), runtime.Version(), hostname())
	log.Printf("archivo de log: %s", path)
	log.Printf("configuración: %s", describeEnv())

	return func() {
		log.Printf("FIN comando=%s duración=%s", command, time.Since(began).Round(time.Millisecond))
		if err == nil {
			_ = pw.Close()
			<-done
			os.Stdout = origStdout
		}
		log.SetOutput(stderr)
		log.SetPrefix("")
		if file != nil {
			_ = file.Close()
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func hostname() string {
	h, _ := os.Hostname()
	return h
}

func describeEnv() string {
	parts := []string{"MONGO_URI=" + MaskURI(os.Getenv("MONGO_URI"))}
	keys := append([]string(nil), envKeys...)
	sort.Strings(keys)
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, " ")
}

// MaskURI oculta usuario y contraseña de una URI de conexión.
func MaskURI(raw string) string {
	if raw == "" {
		return "(por defecto)"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "***"
	}
	if u.User != nil {
		u.User = url.User("***")
	}
	u.RawQuery = ""
	return u.String()
}
