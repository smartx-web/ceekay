package main

import (
 "context"
 "encoding/csv"
 "errors"
 "fmt"
 "io"
 "log"
 "os"
 "strings"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

const expectedWards = 184
const expectedUnits = 4062

func main() {
 if len(os.Args) != 2 { log.Fatal("usage: go run ./cmd/import-polling-units abia_polling_units_verified.csv") }
 url := os.Getenv("DATABASE_URL")
 if url == "" { log.Fatal("DATABASE_URL required") }

 f, err := os.Open(os.Args[1]); if err != nil { log.Fatal(err) }; defer f.Close()
 reader := csv.NewReader(f)
 reader.FieldsPerRecord = 4
 header, err := reader.Read(); if err != nil { log.Fatal(err) }
 expected := []string{"lga", "ward", "code", "name"}
 for i, h := range header {
  if !strings.EqualFold(strings.TrimSpace(h), expected[i]) {
   log.Fatalf("unexpected CSV header %q; expected lga,ward,code,name", strings.Join(header, ","))
  }
 }

 rows := make([][]any, 0, expectedUnits)
 seen := make(map[string]bool)
 wards := make(map[string]bool)
 for line := 2; ; line++ {
  record, e := reader.Read()
  if errors.Is(e, io.EOF) { break }
  if e != nil { log.Fatalf("CSV line %d: %v", line, e) }
  for i := range record {
   record[i] = strings.TrimSpace(record[i])
   if record[i] == "" { log.Fatalf("CSV line %d: empty field", line) }
  }
  key := strings.ToUpper(record[0] + "|" + record[1] + "|" + record[2])
  if seen[key] { log.Fatalf("CSV line %d: duplicate code %s", line, key) }
  seen[key] = true
  if !strings.HasPrefix(record[2], "01-") && !strings.HasPrefix(record[2], "01/") {
   log.Fatalf("CSV line %d: unexpected Abia delimitation code %q", line, record[2])
  }
  wards[strings.ToUpper(record[0]+"|"+record[1])] = true
  rows = append(rows, []any{record[0], record[1], record[2], record[3]})
 }
 if len(rows) != expectedUnits || len(wards) != expectedWards {
  log.Fatalf("CSV validation failed: got %d units and %d wards; expected %d units and %d wards",
   len(rows), len(wards), expectedUnits, expectedWards)
 }

 ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
 defer cancel()
 db, err := pgxpool.New(ctx, url); if err != nil { log.Fatal(err) }; defer db.Close()
 tx, err := db.Begin(ctx); if err != nil { log.Fatal(err) }
 defer tx.Rollback(context.Background())

 // COPY sends all records efficiently in one round trip per batch, within one transaction.
 _, err = tx.Exec(ctx, `CREATE TEMP TABLE import_pus (lga TEXT NOT NULL, ward TEXT NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL) ON COMMIT DROP`)
 if err != nil { log.Fatal(err) }
 copied, err := tx.CopyFrom(ctx, pgx.Identifier{"import_pus"}, []string{"lga", "ward", "code", "name"}, pgx.CopyFromRows(rows))
 if err != nil { log.Fatal(err) }
 if copied != int64(expectedUnits) { log.Fatalf("copied %d rows, expected %d", copied, expectedUnits) }

 // Every CSV ward must match an existing ward in its LGA. Do not create duplicates.
 var missing int
 err = tx.QueryRow(ctx, `
  SELECT count(*) FROM (SELECT DISTINCT lga, ward FROM import_pus) s
  LEFT JOIN lgas l ON upper(trim(l.name)) = upper(trim(s.lga))
  LEFT JOIN wards w ON w.lga_id = l.id AND upper(trim(w.name)) = upper(trim(s.ward))
  WHERE w.id IS NULL`).Scan(&missing)
 if err != nil { log.Fatal(err) }
 if missing != 0 {
  // Report examples without exposing volunteer data.
  examples, e := tx.Query(ctx, `
   SELECT s.lga, s.ward FROM (SELECT DISTINCT lga,ward FROM import_pus) s
   LEFT JOIN lgas l ON upper(trim(l.name)) = upper(trim(s.lga))
   LEFT JOIN wards w ON w.lga_id=l.id AND upper(trim(w.name))=upper(trim(s.ward))
   WHERE w.id IS NULL LIMIT 10`)
  if e != nil { log.Fatal(e) }
  var sample []string
  for examples.Next() { var lga,ward string; if e=examples.Scan(&lga,&ward);e!=nil{log.Fatal(e)};sample=append(sample,fmt.Sprintf("%s / %s",lga,ward)) }
  examples.Close()
  log.Fatalf("%d CSV wards do not match Neon; examples: %s; rolled back", missing, strings.Join(sample, "; "))
 }

 // Reject conflicting existing codes instead of silently reassigning polling units.
 var conflicts int
 err = tx.QueryRow(ctx, `
  SELECT count(*) FROM import_pus s
  JOIN lgas l ON upper(trim(l.name))=upper(trim(s.lga))
  JOIN wards w ON w.lga_id=l.id AND upper(trim(w.name))=upper(trim(s.ward))
  JOIN polling_units p ON p.code=s.code AND p.ward_id<>w.id`).Scan(&conflicts)
 if err != nil { log.Fatal(err) }
 if conflicts != 0 { log.Fatalf("%d codes already exist under different wards; rolled back", conflicts) }

 // Set-based upsert avoids thousands of network round trips.
 _, err = tx.Exec(ctx, `
  INSERT INTO polling_units (ward_id, code, name)
  SELECT w.id, s.code, s.name
  FROM import_pus s
  JOIN lgas l ON upper(trim(l.name))=upper(trim(s.lga))
  JOIN wards w ON w.lga_id=l.id AND upper(trim(w.name))=upper(trim(s.ward))
  ON CONFLICT (ward_id,code) DO UPDATE SET name=EXCLUDED.name`)
 if err != nil { log.Fatal(err) }

 var totalWards, totalUnits int
 err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM wards), (SELECT count(*) FROM polling_units)`).Scan(&totalWards,&totalUnits)
 if err != nil { log.Fatal(err) }
 if totalWards != expectedWards || totalUnits != expectedUnits {
  log.Fatalf("validation failed: got %d wards, %d units; expected %d wards, %d units; rolled back",
   totalWards, totalUnits, expectedWards, expectedUnits)
 }
 if err = tx.Commit(ctx); err != nil { log.Fatal(err) }
 log.Printf("Verified import committed: %d wards and %d polling units", totalWards,totalUnits)
}
