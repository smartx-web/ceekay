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

 "github.com/jackc/pgx/v5/pgxpool"
)

// CSV columns: lga,ward,code,name. Supply only verified INEC records.
// All records are validated before the transaction is committed.
func main() {
 if len(os.Args)!=2 { log.Fatal("usage: go run ./cmd/import-polling-units verified-abia.csv") }
 url:=os.Getenv("DATABASE_URL")
 if url=="" { log.Fatal("DATABASE_URL required") }
 f,err:=os.Open(os.Args[1]);if err!=nil{log.Fatal(err)};defer f.Close()
 reader:=csv.NewReader(f);reader.FieldsPerRecord=4
 header,err:=reader.Read();if err!=nil{log.Fatal(err)}
 expected:=[]string{"lga","ward","code","name"}
 for i,h:=range header {if !strings.EqualFold(strings.TrimSpace(h),expected[i]) {log.Fatalf("unexpected CSV header %q; expected lga,ward,code,name",strings.Join(header,","))}}
 type record struct{lga,ward,code,name string}
 records:=[]record{}
 seen:=map[string]bool{}
 for line:=2;;line++ {
  row,e:=reader.Read();if errors.Is(e,io.EOF){break};if e!=nil{log.Fatalf("CSV line %d: %v",line,e)}
  for i:=range row{row[i]=strings.TrimSpace(row[i]);if row[i]==""{log.Fatalf("CSV line %d: empty field",line)}}
  key:=strings.ToUpper(row[0]+"|"+row[1]+"|"+row[2]);if seen[key]{log.Fatalf("CSV line %d: duplicate code %s",line,key)};seen[key]=true
  if !strings.HasPrefix(row[2],"01-") && !strings.HasPrefix(row[2],"01/"){log.Fatalf("CSV line %d: unexpected Abia delimitation code %q",line,row[2])}
  records=append(records,record{row[0],row[1],row[2],row[3]})
 }
 if len(records)==0{log.Fatal("CSV is empty")}
 ctx,cancel:=context.WithTimeout(context.Background(),3*time.Minute);defer cancel()
 db,err:=pgxpool.New(ctx,url);if err!=nil{log.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{log.Fatal(err)};defer tx.Rollback(ctx)
 wards:=map[string]bool{}
 for _,r:=range records {
  var lgaID,wardID int64
  if err=tx.QueryRow(ctx,"SELECT id FROM lgas WHERE UPPER(name)=UPPER($1)",r.lga).Scan(&lgaID);err!=nil{log.Fatalf("unknown LGA %q: %v",r.lga,err)}
  if err=tx.QueryRow(ctx,"INSERT INTO wards(lga_id,name) VALUES($1,$2) ON CONFLICT(lga_id,name) DO UPDATE SET name=EXCLUDED.name RETURNING id",lgaID,r.ward).Scan(&wardID);err!=nil{log.Fatal(err)}
  _,err=tx.Exec(ctx,"INSERT INTO polling_units(ward_id,code,name) VALUES($1,$2,$3) ON CONFLICT(ward_id,code) DO UPDATE SET name=EXCLUDED.name",wardID,r.code,r.name);if err!=nil{log.Fatal(err)}
  wards[fmt.Sprint(lgaID,"/",wardID)]=true
 }
 var totalWards,totalUnits int
 if err=tx.QueryRow(ctx,"SELECT COUNT(*) FROM wards").Scan(&totalWards);err!=nil{log.Fatal(err)}
 if err=tx.QueryRow(ctx,"SELECT COUNT(*) FROM polling_units").Scan(&totalUnits);err!=nil{log.Fatal(err)}
 if totalWards!=184||totalUnits!=4062 {log.Fatalf("validation failed: got %d wards, %d units; expected 184 wards, 4062 units; rolled back",totalWards,totalUnits)}
 if err=tx.Commit(ctx);err!=nil{log.Fatal(err)}
 log.Printf("Verified import committed: %d wards and %d polling units",totalWards,totalUnits)
}
