# Blog Module TODO

## CRITICAL — Tümü düzeltildi

- [x] **Comment XSS koruması** — `bluemonday.StrictPolicy()` ile content + guest_name sanitize ediliyor, policy CommentService struct'ında cache'leniyor
- [x] **GuestEmail public API'da gizlendi** — `CommentResponse` DTO oluşturuldu, public handler'lar `ToResponse()` kullanıyor, admin handler'lar moderasyon için tam erişim koruyor
- [x] **Media upload post ownership** — `GeneratePresignedUpload` ve `Register`'a `isAdmin` param + `post.AuthorID != uploaderID` kontrolü eklendi
- [x] **ListRevisions/GetRevision yetki kontrolü** — `GetForEdit` ile ownership doğrulaması eklendi
- [x] **ReplaceTags transaction** — `r.db.Transaction()` ile sarıldı

## HIGH — Tümü düzeltildi

- [x] **Post Create atomik** — `db.Transaction()` ile create, stats, revision, tags tek transaction'da. Slug unique violation'da retry mekanizması eklendi (max 3 retry). Swallowed errors çözüldü.
- [x] **RecordShare validation** — `validation.Struct(req)` eklendi, yanıltıcı manuel check kaldırıldı
- [x] **ContentJSON public response'da leak yok** — Doğrulandı: `toPostResponse()` zaten filtriliyor. Ek olarak Create/Update/Publish/Archive handler'ları da `toPostResponse()` kullanacak şekilde güncellendi.
- [x] **Migration CHECK constraint** — `blog_posts.status` ve `blog_comments.status` için CHECK constraint eklendi
- [x] **SSE unsanitized content** — Comment SSE event artık `sanitizedContent` kullanıyor

## MEDIUM

- [x] **Category döngü tespiti** — `detectCycle()` ile parent chain traversal eklendi, depth limit 100
- [x] **Slug race condition** — Transaction içinde unique violation retry mekanizması (3 deneme, her denemede UUID suffix)

## Kalan (scope dışı)

- [ ] **Post.Author populate edilmiyor** — Cross-module integration gerektirir (Identity modülü)
- [ ] **Test coverage ~%8-10** — Ayrı sprint gerektirir
