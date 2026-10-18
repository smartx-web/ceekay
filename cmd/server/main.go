package main
import("log";"os";"github.com/gin-gonic/gin";"github.com/jackc/pgx/v5/pgxpool";"context";"time";"database/sql";"net/mail";"regexp";"errors";"github.com/jackc/pgx/v5/pgconn";"crypto/subtle";"strconv";"strings";"golang.org/x/crypto/bcrypt";"sync";"net/http")
var phonePattern=regexp.MustCompile(`^\+?[0-9]{10,15}$`)
type rateBucket struct { count int; reset time.Time }
var registrationLimiter=struct{sync.Mutex; buckets map[string]rateBucket}{buckets:make(map[string]rateBucket)}
func allowRegistration(ip string) bool {registrationLimiter.Lock();defer registrationLimiter.Unlock();now:=time.Now();if len(registrationLimiter.buckets)>10000 {for key,b:=range registrationLimiter.buckets {if now.After(b.reset){delete(registrationLimiter.buckets,key)}}};b:=registrationLimiter.buckets[ip];if now.After(b.reset){b=rateBucket{reset:now.Add(time.Minute)}};if b.count>=5{return false};b.count++;registrationLimiter.buckets[ip]=b;return true}
type Volunteer struct {FullName string `json:"full_name"`;Phone string `json:"phone"`;Email string `json:"email"`;Community string `json:"community"`;PollingUnitID int64 `json:"polling_unit_id"`;Over18 bool `json:"over_18"`;ElectionDayAvailable bool `json:"election_day_available"`;TrainingAvailable bool `json:"training_available"`;PreviousExperience bool `json:"previous_experience"`;SmartphoneAccess bool `json:"smartphone_access"`;ContactMethod string `json:"contact_method"`;Consent bool `json:"consent"`}
func main(){url:=os.Getenv("DATABASE_URL");if url==""{log.Fatal("DATABASE_URL required")};db,e:=pgxpool.New(context.Background(),url);if e!=nil{log.Fatal(e)};defer db.Close();if e=db.Ping(context.Background());e!=nil{log.Fatal(e)};if e=initializeDB(db);e!=nil{log.Fatalf("database initialization failed: %v",e)}
r:=gin.Default();r.SetTrustedProxies(nil);r.Use(func(c *gin.Context){c.Header("Cache-Control","no-store");c.Header("X-Content-Type-Options","nosniff");c.Header("Referrer-Policy","strict-origin-when-cross-origin");c.Header("X-Frame-Options","DENY");c.Header("Permissions-Policy","camera=(), microphone=(), geolocation=()");c.Next()});r.StaticFile("/","./web/index.html");r.StaticFile("/labour-party-logo.jpeg","./web/labour-party-logo.jpeg");r.StaticFile("/admin","./web/admin.html");r.StaticFile("/privacy","./web/privacy.html");r.GET("/api/health",func(c *gin.Context){ctx,cancel:=context.WithTimeout(c.Request.Context(),2*time.Second);defer cancel();if err:=db.Ping(ctx);err!=nil{c.JSON(503,gin.H{"status":"unavailable"});return};c.JSON(200,gin.H{"status":"ok"})});r.GET("/api/config",func(c *gin.Context){c.JSON(200,gin.H{"registration_enabled":os.Getenv("REGISTRATION_ENABLED")=="true"})})
locations:=func(c *gin.Context,q string,args ...any){rows,e:=db.Query(c.Request.Context(),q,args...);if e!=nil{c.JSON(500,gin.H{"error":"database error"});return};defer rows.Close();items:=[]gin.H{};for rows.Next(){var id int64;var name string;if rows.Scan(&id,&name)!=nil{c.JSON(500,gin.H{"error":"read error"});return};items=append(items,gin.H{"id":id,"name":name})};if err:=rows.Err();err!=nil{c.JSON(500,gin.H{"error":"read error"});return};c.JSON(200,items)}
r.GET("/api/lgas",func(c *gin.Context){locations(c,"SELECT id,name FROM lgas ORDER BY name")})
r.GET("/api/wards",func(c *gin.Context){id,e:=strconv.ParseInt(c.Query("lga_id"),10,64);if e!=nil||id<=0{c.JSON(400,gin.H{"error":"invalid LGA"});return};locations(c,"SELECT id,name FROM wards WHERE lga_id=$1 ORDER BY name",id)})
r.GET("/api/polling-units",func(c *gin.Context){id,e:=strconv.ParseInt(c.Query("ward_id"),10,64);if e!=nil||id<=0{c.JSON(400,gin.H{"error":"invalid ward"});return};locations(c,"SELECT id,code || ' — ' || name FROM polling_units WHERE ward_id=$1 ORDER BY code",id)})
r.POST("/api/volunteers",func(c *gin.Context){if os.Getenv("REGISTRATION_ENABLED")!="true"{c.JSON(503,gin.H{"error":"Registration is not open yet"});return};if !allowRegistration(c.ClientIP()){c.Header("Retry-After","60");c.JSON(http.StatusTooManyRequests,gin.H{"error":"Too many submissions. Please try again later."});return};c.Request.Body=gin.MaxBytesReader(c.Writer,c.Request.Body,16<<10);var v Volunteer;if c.ShouldBindJSON(&v)!=nil||len(strings.TrimSpace(v.FullName))<3||!phonePattern.MatchString(strings.TrimSpace(v.Phone))||v.PollingUnitID<1||!v.Over18||!v.Consent||!v.ElectionDayAvailable||!v.TrainingAvailable{c.JSON(400,gin.H{"error":"invalid or incomplete application"});return};v.FullName=strings.TrimSpace(v.FullName);v.Phone=strings.TrimSpace(v.Phone);v.Email=strings.TrimSpace(v.Email);v.Community=strings.TrimSpace(v.Community);if len(v.FullName)>120||len(v.Email)>254||len(v.Community)>120{c.JSON(400,gin.H{"error":"field too long"});return};if v.Email!=""{a,err:=mail.ParseAddress(v.Email);if err!=nil||a.Address!=v.Email{c.JSON(400,gin.H{"error":"invalid email address"});return}};if v.ContactMethod!="WhatsApp"&&v.ContactMethod!="SMS"&&v.ContactMethod!="Call"{c.JSON(400,gin.H{"error":"invalid contact method"});return};var id int64;e:=db.QueryRow(c.Request.Context(),`INSERT INTO volunteers(full_name,phone,email,community,polling_unit_id,over_18,election_day_available,training_available,previous_experience,smartphone_access,contact_method,consent) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,v.FullName,v.Phone,v.Email,v.Community,v.PollingUnitID,v.Over18,v.ElectionDayAvailable,v.TrainingAvailable,v.PreviousExperience,v.SmartphoneAccess,v.ContactMethod,v.Consent).Scan(&id);if e!=nil{var pe *pgconn.PgError;if errors.As(e,&pe){switch pe.Code{case "23505":c.JSON(409,gin.H{"error":"An application already exists for this phone and polling unit"});return;case "23503":c.JSON(400,gin.H{"error":"Invalid polling unit"});return}};log.Printf("volunteer registration database error: %v",e);c.JSON(500,gin.H{"error":"Unable to save application"});return};c.JSON(201,gin.H{"reference":id})})
admin:=r.Group("/api/admin");admin.Use(func(c *gin.Context){email:=os.Getenv("ADMIN_EMAIL");hash:=os.Getenv("ADMIN_PASSWORD_HASH");u,p,ok:=c.Request.BasicAuth();if email==""||hash==""{c.AbortWithStatusJSON(503,gin.H{"error":"admin not configured"});return};if !ok||subtle.ConstantTimeCompare([]byte(u),[]byte(email))!=1||bcrypt.CompareHashAndPassword([]byte(hash),[]byte(p))!=nil{c.Header("WWW-Authenticate",`Basic realm="CEEKAY"`);c.AbortWithStatusJSON(401,gin.H{"error":"unauthorized"});return};c.Next()})
admin.GET("/volunteers",func(c *gin.Context){
page,err:=strconv.Atoi(c.DefaultQuery("page","1"));if err!=nil||page<1||page>1000000{c.JSON(400,gin.H{"error":"invalid page"});return}
limit,err:=strconv.Atoi(c.DefaultQuery("limit","50"));if err!=nil||limit<1||limit>100{c.JSON(400,gin.H{"error":"limit must be between 1 and 100"});return}
status:=strings.TrimSpace(c.Query("status"));if status!=""{switch status{case "pending","screened","approved","rejected","trained","deployed":default:c.JSON(400,gin.H{"error":"invalid status"});return}}
search:=strings.TrimSpace(c.Query("search"));if len(search)>120{c.JSON(400,gin.H{"error":"search too long"});return}
lga:=strings.TrimSpace(c.Query("lga"));ward:=strings.TrimSpace(c.Query("ward"));if len(lga)>120||len(ward)>120{c.JSON(400,gin.H{"error":"invalid location filter"});return}
where:=` WHERE ($1='' OR v.status=$1) AND ($2='' OR l.name=$2) AND ($3='' OR w.name=$3) AND ($4='' OR v.full_name ILIKE '%'||$4||'%' OR v.phone ILIKE '%'||$4||'%' OR COALESCE(v.email,'') ILIKE '%'||$4||'%' OR COALESCE(v.community,'') ILIKE '%'||$4||'%' OR CAST(v.id AS TEXT)=$4)`
joins:=` FROM volunteers v JOIN polling_units p ON p.id=v.polling_unit_id JOIN wards w ON w.id=p.ward_id JOIN lgas l ON l.id=w.lga_id`
args:=[]any{status,lga,ward,search}
var total int64
if err:=db.QueryRow(c.Request.Context(),`SELECT COUNT(*)`+joins+where,args...).Scan(&total);err!=nil{c.JSON(500,gin.H{"error":"database error"});return}
offset:=(int64(page)-1)*int64(limit)
rows,err:=db.Query(c.Request.Context(),`SELECT v.id,v.full_name,v.phone,v.email,l.name,w.name,p.code,v.status,v.community,v.over_18,v.election_day_available,v.training_available,v.previous_experience,v.smartphone_access,v.contact_method,v.consent,v.created_at`+joins+where+` ORDER BY v.id DESC LIMIT $5 OFFSET $6`,append(args,limit,offset)...)
if err!=nil{c.JSON(500,gin.H{"error":"database error"});return};defer rows.Close()
out:=[]gin.H{}
for rows.Next(){var id int64;var name,phone,lgaName,wardName,pu,statusValue string;var email,community sql.NullString;var over18,electionDay,training,experience,smartphone,consent bool;var contact string;var created time.Time
if rows.Scan(&id,&name,&phone,&email,&lgaName,&wardName,&pu,&statusValue,&community,&over18,&electionDay,&training,&experience,&smartphone,&contact,&consent,&created)!=nil{c.JSON(500,gin.H{"error":"read error"});return}
out=append(out,gin.H{"id":id,"name":name,"phone":phone,"email":email.String,"lga":lgaName,"ward":wardName,"pu":pu,"status":statusValue,"community":community.String,"over_18":over18,"election_day_available":electionDay,"training_available":training,"previous_experience":experience,"smartphone_access":smartphone,"contact_method":contact,"consent":consent,"created_at":created})}
if rows.Err()!=nil{c.JSON(500,gin.H{"error":"read error"});return}
c.JSON(200,gin.H{"items":out,"total":total,"page":page,"limit":limit,"pages":(total+int64(limit)-1)/int64(limit)})
})
admin.GET("/locations",func(c *gin.Context){
rows,err:=db.Query(c.Request.Context(),`SELECT l.name,w.name FROM wards w JOIN lgas l ON l.id=w.lga_id ORDER BY l.name,w.name`)
if err!=nil{c.JSON(500,gin.H{"error":"database error"});return};defer rows.Close()
out:=[]gin.H{};for rows.Next(){var l,w string;if rows.Scan(&l,&w)!=nil{c.JSON(500,gin.H{"error":"read error"});return};out=append(out,gin.H{"lga":l,"ward":w})}
if rows.Err()!=nil{c.JSON(500,gin.H{"error":"read error"});return};c.JSON(200,out)
})
admin.PATCH("/volunteers/:id/status",func(c *gin.Context){var b struct{Status string `json:"status"`};if c.ShouldBindJSON(&b)!=nil{c.JSON(400,gin.H{"error":"invalid request"});return};switch b.Status{case "pending","screened","approved","rejected","trained","deployed":default:c.JSON(400,gin.H{"error":"invalid status"});return};id,e:=strconv.ParseInt(c.Param("id"),10,64);if e!=nil{c.JSON(400,gin.H{"error":"invalid ID"});return};tag,e:=db.Exec(c.Request.Context(),"UPDATE volunteers SET status=$1 WHERE id=$2",b.Status,id);if e!=nil{c.JSON(500,gin.H{"error":"update failed"});return};if tag.RowsAffected()==0{c.JSON(404,gin.H{"error":"volunteer not found"});return};c.JSON(200,gin.H{"ok":true})})
admin.DELETE("/volunteers/:id",func(c *gin.Context){
 if c.GetHeader("X-Confirm-Deletion")!="DELETE"{c.JSON(400,gin.H{"error":"Explicit deletion confirmation required"});return}
 id,e:=strconv.ParseInt(c.Param("id"),10,64);if e!=nil||id<=0{c.JSON(400,gin.H{"error":"invalid ID"});return}
 ctx,cancel:=context.WithTimeout(c.Request.Context(),5*time.Second);defer cancel()
 tag,e:=db.Exec(ctx,"DELETE FROM volunteers WHERE id=$1",id);if e!=nil{log.Printf("volunteer deletion failed for reference %d: %v",id,e);c.JSON(500,gin.H{"error":"deletion failed"});return}
 if tag.RowsAffected()==0{c.JSON(404,gin.H{"error":"volunteer not found"});return}
 log.Printf("admin deleted volunteer reference %d",id)
 c.JSON(200,gin.H{"ok":true,"deleted_reference":id})
})
port:=os.Getenv("PORT");if port==""{port="8080"};log.Fatal(r.Run(":"+port))}

// initializeDB uses idempotent DDL to provision a new database without external access.
func initializeDB(db *pgxpool.Pool) error {
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return err};defer tx.Rollback(ctx)
 statements:=[]string{
 "CREATE TABLE IF NOT EXISTS lgas(id BIGSERIAL PRIMARY KEY,name TEXT UNIQUE NOT NULL)",
 "CREATE TABLE IF NOT EXISTS wards(id BIGSERIAL PRIMARY KEY,lga_id BIGINT NOT NULL REFERENCES lgas(id),name TEXT NOT NULL,UNIQUE(lga_id,name))",
 "CREATE TABLE IF NOT EXISTS polling_units(id BIGSERIAL PRIMARY KEY,ward_id BIGINT NOT NULL REFERENCES wards(id),code TEXT NOT NULL,name TEXT NOT NULL,UNIQUE(ward_id,code))",
 "CREATE TABLE IF NOT EXISTS volunteers(id BIGSERIAL PRIMARY KEY,full_name TEXT NOT NULL,phone TEXT NOT NULL,email TEXT,community TEXT,polling_unit_id BIGINT NOT NULL REFERENCES polling_units(id),over_18 BOOLEAN NOT NULL CHECK(over_18),election_day_available BOOLEAN NOT NULL,training_available BOOLEAN NOT NULL,previous_experience BOOLEAN NOT NULL,smartphone_access BOOLEAN NOT NULL,contact_method TEXT NOT NULL,consent BOOLEAN NOT NULL CHECK(consent),status TEXT NOT NULL DEFAULT 'pending',created_at TIMESTAMPTZ DEFAULT now(),UNIQUE(phone,polling_unit_id))",
 "INSERT INTO lgas(name) VALUES('Aba North'),('Aba South'),('Arochukwu'),('Bende'),('Ikwuano'),('Isiala Ngwa North'),('Isiala Ngwa South'),('Isuikwuato'),('Obingwa'),('Ohafia'),('Osisioma Ngwa'),('Ugwunagbo'),('Ukwa East'),('Ukwa West'),('Umuahia North'),('Umuahia South'),('Umunneochi') ON CONFLICT(name) DO NOTHING",
 }
 for _,s:=range statements{if _,err=tx.Exec(ctx,s);err!=nil{return err}}
 return tx.Commit(ctx)
}
