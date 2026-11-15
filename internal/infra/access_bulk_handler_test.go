package infra

import "testing"

func TestFormatAccessRUT(t *testing.T) {
	if got := formatAccessRUT("123456785"); got != "12.345.678-5" {
		t.Fatalf("got %s", got)
	}
	if got := formatAccessRUT("12.345.678-k"); got != "12.345.678-K" {
		t.Fatalf("got %s", got)
	}
}

func TestReadAccessImportRecordsCSV(t *testing.T) {
	recs, err := readAccessImportRecords(".csv", []byte("\ufeffRUT;Correo;Nombre\n12.345.678-5;a@b.cl;Ana\n"))
	if err != nil || len(recs) != 2 || recs[1][1] != "a@b.cl" || recs[1][2] != "Ana" {
		t.Fatalf("recs=%v err=%v", recs, err)
	}
	if _, err := readAccessImportRecords(".pdf", nil); err == nil {
		t.Fatal("extensión inválida debe fallar")
	}
}
