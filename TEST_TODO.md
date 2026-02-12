# Test TODO — Oncelik Sirasina Gore

Bu dosya, projede eksik olan testleri oncelik sirasina gore listeler.
Her madde bagimsiz bir is birimi olarak yazilabilir.

**Kural:** Her test dosyasi, test ettigi kaynak dosyanin yanina `_test.go` olarak konur.
Testler `internal/test/helpers.go`'daki `test.TestConfig()` ve `test.CreateTestUser*` helper'larini kullanabilir.
Repository testleri gercek DB gerektiriyorsa stub/mock ile yazilmali (unit test).
Mevcut test dosyalarindaki stub pattern'i ornek alinmali (ornek: `internal/modules/identity/service/auth_service_flows_test.go`).

---

## P0 — Kritik (Guvenlik, Auth, Token, Yetkilendirme)

### ~~1. Auth Middleware Testleri~~ (DONE)
**Dosya:** `internal/middleware/auth/auth_middleware_test.go`
**Kaynak:** `internal/middleware/auth/auth_middleware.go`
**Test edilecekler:**
- Authorization header olmadan istek → 401
- Gecersiz bearer token formati → 401
- Suresi dolmus token → 401
- Gecerli token → context'e claims yazilir, next cagirilir
- `RequireRoles()` — dogru rol ile gecis, yanlis rol ile 403
- `RequirePermissions()` — dogru izin ile gecis, yanlis izin ile 403
- `RequireAuth()` — authenticated/unauthenticated senaryolar
- Skip path'ler (health, metrics gibi public endpoint'ler)

### ~~2. Two-Factor Handler Testleri~~ (DONE)
**Dosya:** `internal/modules/identity/api/two_factor_handler_test.go`
**Kaynak:** `internal/modules/identity/api/two_factor_handler.go`
**Test edilecekler:**
- `Enable()` — auth olmadan 401, gecerli kullanici ile QR/secret donmesi
- `Verify()` — gecerli TOTP kodu, gecersiz kod, suresi dolmus kod
- `Disable()` — dogru kod ile devre disi birakma, yanlis kod ile red
- Zaten aktif olan kullanicida tekrar enable denenmesi

### 3. Policy Handler Testleri
**Dosya:** `internal/modules/identity/api/policy_handler_test.go`
**Kaynak:** `internal/modules/identity/api/policy_handler.go`
**Test edilecekler:**
- `AddPolicy()` / `RemovePolicy()` — gecerli ve gecersiz policy
- `AddRoleToUser()` / `RemoveRoleFromUser()` — var olan/olmayan rol
- `GetUserRoles()` / `GetUserPermissions()` — dogru sonuc donmesi
- `CheckPermission()` — allow/deny senaryolari
- `BulkAddPolicies()` — toplu ekleme, kismi basarisizlik
- `ReloadPolicies()` / `SavePolicies()` — basarili/basarisiz

### ~~4. Casbin Service Testleri~~ (DONE)
**Dosya:** `internal/infrastructure/authorization/casbin_service_test.go`
**Kaynak:** `internal/infrastructure/authorization/casbin_service.go`
**Test edilecekler:**
- `Enforce()` — allow/deny sonuclari
- `AddPolicy()` → `Enforce()` ile dogrulama
- `RemovePolicy()` → artik deny donmesi
- `AddRoleForUser()` / `RemoveRoleForUser()` — rol atama/kaldirrma
- `AddRoleInheritance()` — rol hierarchy'si ile enforce
- `GetRolesForUser()` / `GetUsersForRole()` — dogru liste
- `GetPermissionsForUser()` — inherited permission'lar dahil
- `ClearUserPermissions()` — temizlik sonrasi deny

### ~~5. gRPC Auth Interceptor Testleri~~ (DONE)
**Dosya:** `internal/grpc/interceptors_test.go`
**Kaynak:** `internal/grpc/interceptors.go`
**Test edilecekler:**
- `AuthInterceptor()` — metadata'da token yok → Unauthenticated, gecersiz token → Unauthenticated, gecerli token → context'e claims
- `StreamAuthInterceptor()` — ayni senaryolar stream icin
- `RecoveryInterceptor()` — panic durumunda Internal donmesi
- `RateLimitInterceptor()` — limit asimi → ResourceExhausted
- `ErrorInterceptor()` — app error'larin gRPC status code'a cevrimi
- Public method'larin (health check) auth atlayip atlamadigi

### ~~6. Auth Service Eksik Flow Testleri~~ (DONE)
**Dosya:** `internal/modules/identity/service/auth_service_test.go` (yeni dosya)
**Kaynak:** `internal/modules/identity/service/auth_service.go`
**Not:** `auth_service_login_test.go` ve `auth_service_flows_test.go` zaten var. Bu dosya kalan eksikleri kapsar.
**Test edilecekler:**
- `Register()` — basarili kayit, duplicate email, duplicate username
- `VerifyEmail()` — gecerli token, suresi dolmus token, zaten kullanilmis token
- `ResendVerificationEmail()` — kullanici var/yok, zaten verified
- `RequestPasswordReset()` — var olan email, olmayan email (ayni sure donmeli — timing attack onlemi)
- `ResetPassword()` — gecerli token ile sifre degisimi, gecersiz token
- `RefreshToken()` — gecerli refresh token ile yeni token pair, revoked token ile red
- `Logout()` — token blacklist'e eklenmesi

---

## P1 — Yuksek (Core Business Logic, Handler, Service)

### ~~7. Notification Service Testleri~~ (DONE)
**Dosya:** `internal/modules/notification/service/notification_service_test.go`
**Kaynak:** `internal/modules/notification/service/notification_service.go`
**Test edilecekler:**
- `SendNotification()` — email/push/sms/webhook/in-app kanallari
- Provider set edilmemis kanal icin graceful hata
- `GetUserNotifications()` — pagination, filtreleme
- `MarkAsRead()` — basarili/basarisiz
- Retry mantigi (retry_count artisi, max_retries asimi)

### ~~8. Template Service Testleri~~ (DONE)
**Dosya:** `internal/modules/notification/service/template_service_test.go`
**Kaynak:** `internal/modules/notification/service/template_service.go`
**Test edilecekler:**
- Template CRUD (create, get, update, delete)
- `CreateSystemTemplates()` — idempotent calisma
- `CreateCategory()` — unique name constraint
- Template variable validation
- `RenderTemplate()` — degisken substitution, eksik degisken

### ~~9. Enhanced Email Service Testleri~~ (DONE)
**Dosya:** `internal/modules/notification/service/enhanced_email_service_test.go`
**Kaynak:** `internal/modules/notification/service/enhanced_email_service.go`
**Test edilecekler:**
- Template-based email render
- Language fallback (istenen dil yoksa default)
- Variable injection

### ~~10. SSE Service Testleri~~ (DONE)
**Dosya:** `internal/modules/notification/service/sse_service_test.go`
**Kaynak:** `internal/modules/notification/service/sse_service.go`
**Test edilecekler:**
- Event broadcast
- Client subscribe/unsubscribe
- Graceful shutdown

### ~~11. Connection Manager Testleri~~ (DONE)
**Dosya:** `internal/modules/notification/service/connection_manager_test.go`
**Kaynak:** `internal/modules/notification/service/connection_manager.go`
**Test edilecekler:**
- Baglanti ekleme/cikarma
- Kullanici bazli baglanti sayisi
- Concurrent erisim (race condition)

### ~~12. Heartbeat Manager Testleri~~ (DONE)
**Dosya:** `internal/modules/notification/service/heartbeat_manager_test.go`
**Kaynak:** `internal/modules/notification/service/heartbeat_manager.go`
**Test edilecekler:**
- Heartbeat uretimi
- Timeout olan client temizligi

### ~~13. Event Broadcaster Testleri~~ (DONE)
**Dosya:** `internal/modules/notification/service/event_broadcaster_test.go`
**Kaynak:** `internal/modules/notification/service/event_broadcaster.go`
**Test edilecekler:**
- Birden fazla client'a broadcast
- Kapanmis channel'a yazma hatasi

### ~~14. Notification Handler Testleri~~ (DONE)
**Dosya:** `internal/modules/notification/api/notification_handler_test.go`
**Kaynak:** `internal/modules/notification/api/notification_handler.go`
**Test edilecekler:**
- Bildirim olusturma (gecerli/gecersiz payload)
- Bildirim listeleme (pagination)
- Bildirim okuma/silme

### ~~15. Template Handler Testleri~~ (DONE)
**Dosya:** `internal/modules/notification/api/template_handler_test.go`
**Kaynak:** `internal/modules/notification/api/template_handler.go`
**Test edilecekler:**
- Template CRUD endpoint'leri
- Validation hatalari (bos name, gecersiz type)
- Category CRUD

### ~~16. SSE Handler Testleri~~ (DONE)
**Dosya:** `internal/modules/notification/api/sse_handler_test.go`
**Kaynak:** `internal/modules/notification/api/sse_handler.go`
**Test edilecekler:**
- Subscribe endpoint'i
- Connection listing

### ~~17. gRPC Auth Service Testleri~~ (DONE)
**Dosya:** `internal/grpc/services/auth_service_test.go`
**Kaynak:** `internal/grpc/services/auth_service.go`
**Test edilecekler:**
- `Login()` — gecerli/gecersiz credentials
- `Register()` — basarili kayit, duplicate
- `RefreshToken()` — gecerli/revoked token
- `Logout()` — basarili cikis
- gRPC status code mapping (InvalidArgument, Unauthenticated, vb.)

### ~~18. gRPC User Service Testleri~~ (DONE)
**Dosya:** `internal/grpc/services/user_service_test.go`
**Kaynak:** `internal/grpc/services/user_service.go`
**Test edilecekler:**
- `GetUser()` — var olan/olmayan kullanici
- `UpdateUser()` — gecerli/gecersiz veri
- `ListUsers()` — pagination
- `DeleteUser()` — basarili silme

### ~~19. Email Infrastructure Service Testleri~~ (DONE)
**Dosya:** `internal/infrastructure/email/email_service_test.go`
**Kaynak:** `internal/infrastructure/email/email_service.go`
**Test edilecekler:**
- `NewEmailService()` — config validation
- `SendVerificationEmail()` — template render, dogru alici
- `SendPasswordResetEmail()` — template render
- `SendWelcomeEmail()` — template render
- Hata senaryolari (SMTP baglanti hatasi)

### ~~20. Outbox Domain Testleri~~ (DONE)
**Dosya:** `internal/infrastructure/messaging/domain/outbox_test.go`
**Kaynak:** `internal/infrastructure/messaging/domain/outbox.go`
**Test edilecekler:**
- State transition'lar: `MarkAsProcessing()`, `MarkAsSent()`, `MarkAsFailed()`
- `IsPending()`, `IsProcessing()` — state check'ler
- `CanRetry()` — retry_count < max_retries, suresi dolmus
- `IncrementRetry()` — counter artisi, next_retry_at hesabi
- `MoveToDLQ()` — dead letter olusturma
- `HasExpired()` — TTL mantigi
- `GetPriorityLevel()` — priority string donusu

### ~~21. Event Dispatcher Testleri~~ (DONE)
**Dosya:** `internal/infrastructure/messaging/events/event_dispatcher_test.go`
**Kaynak:** `internal/infrastructure/messaging/events/event_dispatcher.go`
**Test edilecekler:**
- Event dispatch
- Handler registration
- Hata durumunda davranis

---

## P2 — Orta (Infrastructure, Cache, Resilience)

### ~~22. Circuit Breaker Testleri~~ (DONE)
**Dosya:** `internal/infrastructure/circuitbreaker/breaker_test.go`
**Kaynak:** `internal/infrastructure/circuitbreaker/breaker.go`
**Test edilecekler:**
- Closed → Open gecisi (threshold asilinca)
- Open → Half-Open gecisi (timeout sonrasi)
- Half-Open → Closed (basarili istek)
- Half-Open → Open (basarisiz istek)
- `Execute()` — closed'da fonksiyon calistirir, open'da hemen hata
- `GetState()` / `GetStats()` — dogru degerler
- `Reset()` — state sifirlama
- Concurrent erisim

### ~~23. Redis Client Testleri~~ (DONE)
**Dosya:** `internal/infrastructure/cache/redis_test.go`
**Kaynak:** `internal/infrastructure/cache/redis.go`
**Not:** Redis baglantisi olmadan test edilebilecek birimler (config validation, circuit breaker entegrasyonu). Gercek Redis gerektiren testler build tag ile ayrilmali.
**Test edilecekler:**
- `NewRedisClient()` — config validation
- Circuit breaker state'leri
- `Close()` — graceful shutdown

### ~~24. Token Blacklist Testleri~~ (DONE)
**Dosya:** `internal/infrastructure/cache/token_blacklist_test.go`
**Kaynak:** `internal/infrastructure/cache/token_blacklist.go`
**Test edilecekler:**
- Token ekleme ve sorgulama
- TTL sonrasi otomatik silme mantigi

### ~~25. Rate Limiter Testleri~~ (DONE)
**Dosya:** `internal/infrastructure/cache/rate_limiter_test.go`
**Kaynak:** `internal/infrastructure/cache/rate_limiter.go`
**Test edilecekler:**
- Limit altinda izin verme
- Limit asiminda red
- Window reset sonrasi tekrar izin

### ~~26. Prometheus Metrics Testleri~~ (DONE)
**Dosya:** `internal/infrastructure/metrics/prometheus_test.go`
**Kaynak:** `internal/infrastructure/metrics/prometheus.go`
**Test edilecekler:**
- `InitMetrics()` — metrik register
- Counter increment (RecordHTTPRequest, RecordEmailSent vb.)
- Histogram observe
- Gauge set

### ~~27. Database Package Testleri~~ (DONE)
**Dosya:** `internal/infrastructure/database/database_test.go`
**Kaynak:** `internal/infrastructure/database/database.go`
**Test edilecekler:**
- `RunMigrations()` — goose.Up cagrisi (mock sql.DB ile)
- `MigrationStatus()` — status raporu
- `HealthCheck()` — ping basarili/basarisiz

---

## P3 — Dusuk (Config, Logger, DTO)

### 28. Config Testleri
**Dosya:** `internal/core/config/config_test.go`
**Kaynak:** `internal/core/config/config.go`
**Test edilecekler:**
- `Load()` — default degerler, env override
- `GetDSN()` — dogru format
- `IsDevelopment()` / `IsProduction()` / `IsStaging()` — dogru bool
- Validation hatalari (eksik required alanlar)

### 29. Logger Testleri
**Dosya:** `internal/core/logger/logger_test.go`
**Kaynak:** `internal/core/logger/logger.go`
**Test edilecekler:**
- `Initialize()` — level/format/output ayarlari
- `Get()` — singleton donmesi
- Log level filtreleme (debug mesaji info seviyesinde gorunmemeli)

### 30. Core Errors Testleri
**Dosya:** `internal/core/errors/errors_test.go`
**Kaynak:** `internal/core/errors/errors.go`
**Test edilecekler:**
- Hata olusturma (NewAppError vb.)
- Error code mapping
- HTTP status code donusu

### 31. Validation Testleri
**Dosya:** `internal/core/validation/validation_test.go`
**Kaynak:** `internal/core/validation/validation.go`
**Test edilecekler:**
- `Init()` — validator kurulumu
- Custom validation rule'lari
- Hata mesaji formatlama

---

## Ozet

| Oncelik | Sayi | Aciklama |
|---------|------|----------|
| P0      | 6    | Auth, 2FA, RBAC, middleware, gRPC interceptors |
| P1      | 15   | Notification, template, SSE, gRPC services, email, outbox |
| P2      | 6    | Circuit breaker, Redis, rate limiter, metrics, database |
| P3      | 4    | Config, logger, errors, validation |
| **Toplam** | **31** | |
