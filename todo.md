 ---
  YÜKSEK

  4. Sınırsız goroutine üretimi — bildirim servisinde
  go s.processNotification() çağrıları sınırlandırılmamış. Yüksek trafik = goroutine patlaması.
  - internal/modules/notification/service/notification_service.go:166,209,304,333

  8. Şifre değişikliği/sıfırlama mevcut oturumları geçersiz kılmıyor
  Eski token'lar çalışmaya devam ediyor.
  - internal/modules/identity/service/auth_service.go:465-498 (ChangePassword)
  - internal/modules/identity/service/auth_service.go:546-604 (ResetPassword)

  ---
  ORTA

  15. gRPC ListUsers filtre parametreleri yok sayılıyor
  Proto'da sort, search, roles, only_active var ama hiçbiri uygulanmıyor.
  - internal/grpc/services/user_service.go:66-111

  16. Config Viper Get metodları farklı instance kullanıyor
  GetBool/GetInt global viper, Load local viper.
  - internal/core/config/config.go:494-511

  17. gRPC loadConfig tekrarlanan mantık
  cmd/grpc/main.go:193-287 — config.Load() yerine kendi config'ini elle oluşturuyor.

  18. Prometheus middleware raw path — yüksek kardinalite
  c.Path() kullanıyor, /users/123 ve /users/456 ayrı metrik oluyor.
  - internal/infrastructure/metrics/prometheus.go:522-536

  19. SSE servisi shutdown'da Stop() çağrılmıyor
  cmd/api/main.go:127-154 — Shutdown sekansında SSE temizliği yok.

  ---
  DÜŞÜK (Backlog)
  ┌─────┬─────────────────────────────────────────────────────┬─────────────────────────────────────┐
  │  #  │                        Sorun                        │                Dosya                │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 21  │ toGRPCError iki dosyada duplicate                   │ interceptors.go + auth_service.go   │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 23  │ Goroutine'larda recovery eksik                      │ api_key_service.go:93               │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 24  │ parseDurations() hataları sessizce atlıyor          │ config/config.go:425-458            │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 25  │ outbox_repository.GetStatistics() hata kontrolü yok │ outbox_repository.go:245-250        │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 27  │ CreatePermission hala placeholder                   │ permission_handler.go:143-157       │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 28  │ Template BulkUpdate/Export placeholder              │ template_handler.go:448-465,522-533 │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 29  │ Email attachment desteği yok (skip ediliyor)        │ email_service.go:155-156            │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 30  │ Scheduled email DB'ye kaydediliyor ama işlenmiyor   │ notification_service.go:162         │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 31  │ Health check'te email/storage/SSE kontrolü yok      │ server.go:391-452                   │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 32  │ Tracing middleware hala kullanılmıyor               │ api/middleware/tracing.go           │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 33  │ Ölü kod: TracingConfig, ServerOptions               │ opentelemetry.go, grpc/server.go    │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 34  │ gRPC global rate limiter hardcoded (100/10)         │ interceptors.go:53                  │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 35  │ EventBroadcaster priority queue non-deterministik   │ event_broadcaster.go:333-351        │
  ├─────┼─────────────────────────────────────────────────────┼─────────────────────────────────────┤
  │ 36  │ GetNotificationsSince memory'de filtreliyor         │ notification_service.go:229-246     │