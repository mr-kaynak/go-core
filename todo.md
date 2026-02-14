Admin Panel Tamamlama ve Eksik Endpoint Planı

 Bağlam

 go-core şu an 101 HTTP endpoint'e sahip olgun bir enterprise uygulama iskeleti. Ancak admin paneli tamamlanmamış: bazı endpoint'ler placeholder (501/boş
 response), bazı servis metodları endpoint'e bağlanmamış, ve admin paneli için kritik yönetim endpoint'leri eksik. Bu plan, mevcut kalıplara (handler → service →
 repository) sadık kalarak tüm eksikleri giderir.

 ---
 Faz 1: Mevcut Placeholder'ları Düzeltme (Hızlı Kazanımlar)

 1.1 CreatePermission — Gerçek implementasyon (P0)

 - Dosya: internal/modules/identity/api/permission_handler.go:188-215
 - Sorun: Validation yapıyor ama DB'ye kaydetmiyor, placeholder mesajı dönüyor
 - Çözüm: h.permRepo.Create() ile gerçek permission oluşturma, audit log kaydı
 - Not: permRepo zaten handler'da mevcut, Create metodu repository interface'inde var

 1.2 CreateNotification — Admin bildirim gönderme (P0)

 - Dosya: internal/modules/notification/api/notification_handler.go:93-98
 - Sorun: 501 Not Implemented dönüyor
 - Çözüm: Admin yetkisi kontrolü ekle, notificationService.SendNotification() ile belirli kullanıcıya bildirim gönderme. Mevcut SendNotification servisi tam
 çalışır durumda (email, push, in-app, webhook destekli)
 - Not: Broadcast zaten POST /admin/sse/broadcast ile mevcut; bu endpoint tekil kullanıcıya bildirim göndermek için

 1.3 BulkUpdateTemplates — Toplu template güncelleme (P1)

 - Dosya: internal/modules/notification/api/template_handler.go:660-674
 - Sorun: Parse yapıyor ama gerçek güncelleme yapmıyor
 - Çözüm:
   - TemplateService'e BulkUpdate(ids []uuid.UUID, updates) metodu ekle
   - Repository'ye BulkUpdate metodu ekle
   - Whitelist: sadece is_active ve category_id güncellenebilir (güvenlik)

 1.4 ExportTemplates — Template dışa aktarma (P1)

 - Dosya: internal/modules/notification/api/template_handler.go:739-749
 - Sorun: ID parse yok, boş array dönüyor
 - Çözüm:
   - c.Query("ids") ile virgülle ayrılmış UUID'leri parse et
   - templateService.ListTemplates() ile template'leri getir
   - Content-Disposition: attachment; filename=templates.json header'ı ekle

 ---
 Faz 2: Temel Admin Endpoint'leri

 2.1 Admin Dashboard (P0) — YENİ

 - Endpoint: GET /api/v1/admin/dashboard
 - Yeni dosya: internal/modules/identity/api/admin_handler.go
 - Response:
 users: { total, active, inactive, locked, today_registrations }
 notifications: { pending, sent, failed }
 sse: { active_connections, channels }
 system: { uptime, version }
 - Mevcut kullanılacak: userRepo.Count(), notificationRepo.CountByStatus() (yeni), sseService.GetStats() (mevcut)
 - Yeni repo metodları: UserRepo.CountByStatus(), NotificationRepo.CountByStatus()

 2.2 Detaylı System Health (P0) — YENİ

 - Endpoint: GET /api/v1/admin/system/health
 - Dosya: admin_handler.go içinde
 - Response: DB (pool stats), Redis (ping + info), RabbitMQ, Email (configured?), Storage (type + configured?), SSE (healthy + connection count)
 - Mevcut kullanılacak: db.DB().Stats(), rc.HealthCheck(), sseService.IsHealthy() (mevcut)

 2.3 Audit Log Gelişmiş Filtreleme (P1)

 - Endpoint: GET /api/v1/admin/audit-logs (mevcut genişletme)
 - Dosyalar:
   - internal/modules/identity/api/user_handler.go — start_date, end_date query params ekleme
   - internal/modules/identity/repository/audit_log_repository.go — AuditLogListFilter'a StartDate, EndDate ekleme
   - internal/modules/identity/repository/audit_log_repository_impl.go — WHERE koşulları

 2.4 Orphan Servis Metodlarını Endpoint'e Bağlama (P1)

 - AuditService.GetActionLogs() → GET /api/v1/admin/audit-logs/by-action?action=user.login
 - AuditService.GetResourceLogs() → GET /api/v1/admin/audit-logs/by-resource?resource=user&resource_id=xxx
 - Dosya: admin_handler.go veya mevcut user_handler.go admin routes içinde

 2.5 Admin API Key Yönetimi (P1) — YENİ

 - Endpoint: GET /api/v1/admin/api-keys — Tüm API key'leri listele
 - Endpoint: DELETE /api/v1/admin/api-keys/:id — Herhangi bir key'i iptal et
 - Yeni repo metodu: APIKeyRepository.GetAll(offset, limit) ([]*APIKey, int64, error)
 - Yeni servis metodu: APIKeyService.ListAll(), APIKeyService.AdminRevoke(keyID)

 2.6 Admin Session Yönetimi (P1) — YENİ

 - Endpoint: GET /api/v1/admin/sessions — Tüm aktif session'ları listele
 - Endpoint: DELETE /api/v1/admin/sessions/user/:userId — Kullanıcıyı zorla çıkış yaptır
 - Mevcut kullanılacak: userRepo.RevokeAllUserRefreshTokens(userID) (force logout single user)
 - Yeni repo metodu: GetAllActiveSessions(offset, limit), CountActiveSessions()

 2.7 Email Log Görüntüleme (P2) — YENİ

 - Endpoint: GET /api/v1/admin/email-logs
 - Mevcut: domain.EmailLog modeli ve notificationRepo.GetEmailLogsByUser/Notification mevcut
 - Yeni repo metodu: ListEmailLogs(offset, limit, status)

 2.8 Test Email Gönderme (P2) — YENİ

 - Endpoint: POST /api/v1/admin/email/test
 - Mevcut kullanılacak: emailSvc.Send() direkt çağrı

 ---
 Faz 3: İleri Admin Özellikleri

 3.1 Kullanıcı Export (P2) — YENİ

 - Endpoint: GET /api/v1/admin/users/export?format=json|csv
 - Mevcut kullanılacak: userService.AdminListUsers(filter) büyük limit ile

 3.2 Toplu Kullanıcı İşlemleri (P2) — YENİ

 - Endpoint: POST /api/v1/admin/users/bulk-status — {user_ids: [...], status: "active"}
 - Endpoint: POST /api/v1/admin/users/bulk-role — {user_ids: [...], role_id: "..."}
 - Mevcut kullanılacak: userService.AdminUpdateStatus(), userService.AdminAssignRole() döngüde

 3.3 Bildirim İstatistikleri (P2) — YENİ

 - Endpoint: GET /api/v1/admin/notifications/stats
 - Yeni repo metodları: CountByStatus(), CountByType() — basit GROUP BY query'leri

 3.4 Audit Log Export (P2)

 - Endpoint: GET /api/v1/admin/audit-logs/export
 - Mevcut kullanılacak: auditService.ListAllLogs() büyük limit ile

 3.5 Notification Queue Yönetimi (P2)

 - Endpoint: POST /api/v1/admin/notifications/retry-failed
 - Endpoint: POST /api/v1/admin/notifications/process-pending
 - Mevcut kullanılacak: notificationService.RetryFailedNotifications(), ProcessPendingNotifications() — zaten implementasyonu mevcut, sadece handler yazılacak

 ---
 Faz 4: Altyapı İyileştirmeleri

 4.1 Admin IP Whitelist Middleware (P2) — Opsiyonel

 - Yeni dosya: internal/middleware/auth/ip_whitelist.go
 - Config'e AdminIPWhitelist []string eklenir
 - server.go'da admin grubuna admin.Use(ipWhitelistMiddleware) eklenir

 4.2 Auth Endpoint Rate Limit (P2)

 - Login/register için ayrı rate limiter: login 5/dk, register 3/dk
 - server.go'da auth route grubuna ek limiter.New() instance'ı

 ---
 Dosya Değişiklik Özeti

 Yeni Dosyalar (2)

 Dosya: internal/modules/identity/api/admin_handler.go
 İçerik: Dashboard, system health, API key admin, session admin, email admin, bildirim stats, export'lar, queue yönetimi
 ────────────────────────────────────────
 Dosya: internal/middleware/auth/ip_whitelist.go
 İçerik: IP whitelist middleware (Faz 4, opsiyonel)

 Mevcut Dosya Değişiklikleri

 ┌──────────────────────────────────────────────────────────────────────────┬───────────────────────────────────────────────────────────────────┐
 │                                  Dosya                                   │                            Değişiklik                             │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/infrastructure/server/server.go                                 │ AdminHandler oluşturma ve admin grubuna register etme (~20 satır) │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/identity/api/permission_handler.go                      │ CreatePermission placeholder → gerçek impl (~10 satır)            │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/notification/api/notification_handler.go                │ CreateNotification 501 → gerçek impl (~25 satır)                  │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/notification/api/template_handler.go                    │ BulkUpdate + Export placeholder → gerçek impl (~40 satır)         │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/notification/service/template_service.go                │ BulkUpdate, ExportTemplates metodları                             │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/notification/repository/template_repository.go          │ BulkUpdate interface metodu                                       │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/notification/repository/template_repository_impl.go     │ BulkUpdate implementasyonu                                        │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/identity/repository/audit_log_repository.go             │ Filter'a StartDate/EndDate                                        │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/identity/repository/audit_log_repository_impl.go        │ Tarih filtresi WHERE koşulları                                    │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/identity/repository/api_key_repository.go               │ GetAll interface metodu                                           │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/identity/repository/api_key_repository_impl.go          │ GetAll implementasyonu                                            │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/identity/repository/user_repository.go                  │ GetAllActiveSessions, CountByStatus                               │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/identity/repository/user_repository_impl.go             │ Session ve count implementasyonları                               │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/identity/service/api_key_service.go                     │ ListAll, AdminRevoke                                              │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/notification/repository/notification_repository.go      │ ListEmailLogs, CountByStatus, CountByType                         │
 ├──────────────────────────────────────────────────────────────────────────┼───────────────────────────────────────────────────────────────────┤
 │ internal/modules/notification/repository/notification_repository_impl.go │ İmplementasyonlar                                                 │
 └──────────────────────────────────────────────────────────────────────────┴───────────────────────────────────────────────────────────────────┘

 ---
 Yeni Admin Endpoint Özeti (Toplam 18 yeni endpoint)

 ┌─────┬────────┬──────────────────────────────────────┬─────┬─────────┐
 │  #  │ Method │                 Path                 │ Faz │ Öncelik │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 1   │ GET    │ /admin/dashboard                     │ F2  │ P0      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 2   │ GET    │ /admin/system/health                 │ F2  │ P0      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 3   │ GET    │ /admin/api-keys                      │ F2  │ P1      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 4   │ DELETE │ /admin/api-keys/:id                  │ F2  │ P1      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 5   │ GET    │ /admin/sessions                      │ F2  │ P1      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 6   │ DELETE │ /admin/sessions/user/:userId         │ F2  │ P1      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 7   │ GET    │ /admin/audit-logs/by-action          │ F2  │ P1      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 8   │ GET    │ /admin/audit-logs/by-resource        │ F2  │ P1      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 9   │ GET    │ /admin/email-logs                    │ F2  │ P2      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 10  │ POST   │ /admin/email/test                    │ F2  │ P2      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 11  │ GET    │ /admin/users/export                  │ F3  │ P2      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 12  │ POST   │ /admin/users/bulk-status             │ F3  │ P2      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 13  │ POST   │ /admin/users/bulk-role               │ F3  │ P2      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 14  │ GET    │ /admin/notifications/stats           │ F3  │ P2      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 15  │ GET    │ /admin/audit-logs/export             │ F3  │ P2      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 16  │ POST   │ /admin/notifications/retry-failed    │ F3  │ P2      │
 ├─────┼────────┼──────────────────────────────────────┼─────┼─────────┤
 │ 17  │ POST   │ /admin/notifications/process-pending │ F3  │ P2      │
 └─────┴────────┴──────────────────────────────────────┴─────┴─────────┘

 - 4 mevcut placeholder düzeltmesi (Faz 1)
