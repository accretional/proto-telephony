package carrier

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// WriteCSV writes ds as one CSV file per table (networks.csv, amendments.csv,
// ...) into dir, each with a header row. NULLs are written as empty fields.
// The files are built in dir+".tmp" and swapped in, so a failed run leaves
// the previous output in place.
func WriteCSV(dir string, ds *Dataset) error {
	ds.Meta["written_at"] = time.Now().UTC().Format(time.RFC3339)
	tmp := dir + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	for _, tb := range tables(ds) {
		if err := writeCSVFile(filepath.Join(tmp, tb.name+".csv"), tb); err != nil {
			os.RemoveAll(tmp)
			return err
		}
	}
	old := dir + ".old"
	os.RemoveAll(old)
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		return err
	}
	return os.RemoveAll(old)
}

func writeCSVFile(path string, tb table) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.Write(tb.cols)
	rec := make([]string, len(tb.cols))
	for _, r := range tb.rows {
		for i, v := range r {
			rec[i] = csvField(v)
		}
		w.Write(rec)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return fmt.Errorf("%s: %w", path, err)
	}
	return f.Close()
}

func csvField(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		if x {
			return "1"
		}
		return "0"
	}
	return fmt.Sprint(v)
}

// ReadCSV loads a directory written by WriteCSV into an in-memory SQLite
// database with the same schema as WriteSQLite. Empty fields in nullable
// columns become NULL.
func ReadCSV(dir string) (*sql.DB, error) {
	var tbs []table
	for _, spec := range tables(&Dataset{Meta: map[string]string{}}) {
		f, err := os.Open(filepath.Join(dir, spec.name+".csv"))
		if err != nil {
			return nil, err
		}
		recs, err := csv.NewReader(f).ReadAll()
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s.csv: %w", spec.name, err)
		}
		if len(recs) == 0 || len(recs[0]) != len(spec.cols) {
			return nil, fmt.Errorf("%s.csv: header doesn't match columns %v", spec.name, spec.cols)
		}
		for i, c := range spec.cols {
			if recs[0][i] != c {
				return nil, fmt.Errorf("%s.csv: column %d is %q, want %q", spec.name, i+1, recs[0][i], c)
			}
		}
		for _, rec := range recs[1:] {
			row := make([]any, len(rec))
			for i, v := range rec {
				if v == "" && spec.nullable[spec.cols[i]] {
					row[i] = nil
				} else {
					row[i] = v
				}
			}
			spec.rows = append(spec.rows, row)
		}
		tbs = append(tbs, spec)
	}
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one connection = one in-memory database
	if err := load(db, tbs); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
