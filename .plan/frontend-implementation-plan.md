# Frontend Implementation Planı — core-ui

## Mevcut Durum

Frontend'te şu sayfalar **zaten var** ve çalışıyor:
- Auth (login, signup, forgot/reset password, verify email)
- Dashboard (admin/user dual view, SSE monitor, activity feed)
- Profile, Notifications (list + preferences)
- Admin: Users CRUD, Roles CRUD, Permissions CRUD, Audit Logs (SSE), API Keys

**Eksik olan büyük parçalar:**
- Template yönetimi (hiç yok — sadece dashboard'da usage widget var)
- Admin panel genişletmeleri (system health, session yönetimi, email logs, bulk operasyonlar, export'lar)
- Mevcut sayfaların UX iyileştirmeleri (rol/permission tabloları)

---

## FAZA 1: Template Yönetim Sistemi

### 1.1 Template Listesi

**Kullanıcı senaryosu:** Admin, mevcut tüm template'leri görmek, filtrelemek ve yönetmek istiyor.

- Sol tarafta kategori sidebar'ı (ağaç yapısında, `GET /templates/categories`)
- Sağda template listesi (kartlar veya tablo, `GET /templates`)
- Her kartta: isim, tip (email/push/in_app/webhook/sms) badge'i, aktif/pasif durumu, kullanım sayısı, son güncelleme
- Filtreleme: tip, aktif/pasif, kategori
- Arama: isim bazlı
- Toplu işlemler: seçili template'leri aktif/pasif yap, kategori değiştir (`POST /templates/bulk-update`)
- Aksiyonlar: düzenle, klonla (`POST /templates/:id/clone`), sil
- Import/Export butonları (`GET /templates/export`, `POST /templates/import`)

### 1.2 Template Editörü (Split View)

**Kullanıcı senaryosu:** Admin, bir email template'inin body'sini düzenlerken sağ tarafta anlık olarak nasıl göründüğünü görmek istiyor. Değişkenleri (`{{.Username}}`, `{{.AppName}}`) test verileriyle doldurup sonucu görmek istiyor.

**Layout:**
```
┌─────────────────────────┬─────────────────────────┐
│  EDITOR (sol %50)       │  PREVIEW (sağ %50)      │
│                         │                         │
│  [Tab: Body | Subject]  │  Rendered HTML/Text      │
│                         │                         │
│  CodeMirror/Monaco      │  iframe veya sanitized   │
│  veya textarea          │  HTML render             │
│  syntax highlighted     │                         │
│                         │  ─────────────────────  │
│                         │  Test Variables:         │
│                         │  Username: [John]        │
│                         │  AppName: [MyApp]        │
│                         │  VerificationURL: [...]  │
│                         │                         │
│  ─────────────────────  │  [Preview Güncelle]     │
│  Metadata:              │                         │
│  İsim: [___________]   │                         │
│  Tip:  [email ▼]       │                         │
│  Kategori: [system ▼]  │                         │
│  Aktif: [toggle]        │                         │
│  Taglar: [chip input]   │                         │
└─────────────────────────┴─────────────────────────┘
```

**Akış:**
1. Template açılır → `GET /templates/:id` ile mevcut veri yüklenir
2. Sol tarafta body düzenlenir (Go template syntax: `{{.Variable}}`)
3. Sağ taraftaki "Test Variables" alanları template'in variable tanımlarından otomatik oluşur (`GET /templates/:id/variables`)
4. Her variable için input alanı, default değer varsa pre-filled
5. "Preview" butonuna basılınca `POST /templates/preview` çağrılır, render edilmiş HTML sağ panelde gösterilir
6. Kaydet → `PUT /templates/:id`

**Yeni template oluşturma** da aynı layout'u kullanır, sadece boş başlar. Sistem template'leri (`is_system: true`) readonly gösterilir, düzenleme engellenir.

### 1.3 Variable Yönetimi

**Kullanıcı senaryosu:** Admin, template'e yeni bir değişken eklemek istiyor (mesela `{{.CompanyLogo}}`). Her değişkenin tipi, zorunlu olup olmadığı ve varsayılan değeri belirlenebilmeli.

- Template editör sayfasında alt kısımda veya ayrı bir tab'da
- Tablo: İsim | Tip (string/number/boolean/date) | Zorunlu | Varsayılan Değer | Açıklama
- Satır içi düzenleme (inline edit)
- Yeni değişken ekleme satırı (son satır boş input olarak)
- Sil butonu (hover'da görünür)
- Endpoint'ler: `GET/POST /templates/:id/variables`, `PUT /templates/:id/variables/:varId`
- Değişken eklendiğinde/silindiğinde preview panelindeki test alanları da güncellenir

### 1.4 Template Kategorileri

**Kullanıcı senaryosu:** Template'leri mantıksal gruplara ayırmak (system, marketing, transactional).

- Template listesi sayfasının sol sidebar'ında
- Kategori ekleme/düzenleme inline (isim + açıklama)
- Sürükle-bırak sıralama (dnd-kit zaten projede var)
- Kullanımda olan kategori silinemez (backend zaten kontrol ediyor)
- Endpoint'ler: `GET/POST/PUT/DELETE /templates/categories`

### 1.5 Test Email Gönderimi

**Kullanıcı senaryosu:** Admin, template'i düzenledikten sonra gerçekten nasıl göründüğünü email olarak test etmek istiyor.

- Template editörde "Test Email Gönder" butonu
- Dialog açılır: alıcı email adresi input'u
- Template'i önce render eder (`POST /templates/render`), sonra test email gönderir (`POST /admin/email/test`)
- Sonuç: başarılı/hatalı toast

---

## FAZA 2: Rol & Permission UX İyileştirmesi

### 2.1 Roller Tablosu Yeniden Tasarımı

**Mevcut sorun:** Basit bir liste, permission sayısı sadece rakam olarak gösteriliyor. Hangi yetkiler atanmış anlaşılmıyor.

**Yeni tasarım:**
- Genişletilebilir satırlar (expandable rows): satıra tıklayınca altında atanmış permission'lar badge olarak görünür
- Permission badge'leri kategori rengine göre renklendirilir
- Hızlı aksiyon: "Permission Yönet" butonu → sağdan sheet (drawer) açılır
- Sheet içinde: sol taraf mevcut permission'lar (çıkarmak için X), sağ taraf atanabilir permission'lar (eklemek için +)
- Drag-drop ile permission sıralama (opsiyonel)
- Rol kalıtımı görselleştirmesi: "Bu rol X rolünden miras alır" bilgi kartı

### 2.2 Permission Tablosu Yeniden Tasarımı

**Mevcut sorun:** Düz bir liste, kategori filtresi var ama görsel olarak kategoriler ayrışmıyor.

**Yeni tasarım:**
- Kategori bazlı gruplama (accordion style): her kategori bir başlık, altında o kategorinin permission'ları
- Her permission satırında: isim, açıklama, kaç rolde kullanıldığı (badge)
- Kullanıldığı roller hover'da tooltip olarak gösterilir
- Bulk seçim: birden fazla permission seçip toplu silme veya kategori değiştirme
- Yeni permission ekleme satırı her kategorinin altında inline (hızlı ekleme)

### 2.3 Rol-Permission Matris Görünümü (Opsiyonel)

**Kullanıcı senaryosu:** Admin, hangi rolün hangi yetkiye sahip olduğunu tek bakışta görmek istiyor.

- Yatay: roller, dikey: permission'lar
- Kesişimlerde checkbox
- Checkbox toggle'layınca `POST/DELETE /roles/:id/permissions/:permission_id`
- Büyük veri setlerinde virtual scroll
- Bu ayrı bir "Matris" tab'ı olarak rol veya permission sayfasına eklenebilir

---

## FAZA 3: Admin Panel Genişletmeleri

### 3.1 Sistem Sağlığı Sayfası

**Kullanıcı senaryosu:** Admin, tüm sistem bileşenlerinin durumunu tek sayfada görmek istiyor.

- Üstte genel durum: yeşil/sarı/kırmızı büyük badge (healthy/degraded/unhealthy)
- Altında kart grid: Database, Redis, SSE, Email, Storage
- Her kartta: durum ikonu, detaylar (bağlantı sayısı, ping süresi vs.)
- Auto-refresh (30 saniyede bir)
- `GET /admin/system/health`

### 3.2 Session Yönetimi

**Kullanıcı senaryosu:** Admin, sistemdeki tüm aktif oturumları görmek ve şüpheli oturumları sonlandırmak istiyor.

- Tablo: kullanıcı, IP adresi, user agent, oluşturulma, son kullanma tarihi
- IP bazlı gruplama veya filtreleme (aynı IP'den çok fazla oturum → uyarı)
- "Kullanıcının tüm oturumlarını kapat" butonu (user satırında)
- `GET /admin/sessions`, `DELETE /admin/sessions/user/:userId`

### 3.3 Email Logları

**Kullanıcı senaryosu:** Admin, gönderilen email'lerin durumunu takip etmek istiyor (hangisi gönderildi, hangisi başarısız oldu).

- Tablo: alıcı, konu, durum (sent/failed/pending), tarih
- Status filtresi (tabs: All | Sent | Failed | Pending)
- Başarısız email'lerde hata detayı (expandable row)
- `GET /admin/email-logs?status=X`

### 3.4 Toplu Kullanıcı İşlemleri

**Kullanıcı senaryosu:** Admin, 50 kullanıcıyı aynı anda "inactive" yapmak veya hepsine aynı rolü atamak istiyor.

- Mevcut users tablosuna checkbox kolonu eklenir
- Seçili kullanıcılar için toolbar belirir: "Durum Değiştir" | "Rol Ata"
- Durum değiştir: dropdown (active/inactive/locked) → `POST /admin/users/bulk-status`
- Rol ata: rol seçim dropdown'u → `POST /admin/users/bulk-role`
- Sonuç: başarılı/başarısız sayıları toast olarak (207 partial response handling)

### 3.5 Export İşlemleri

**Kullanıcı senaryosu:** Admin, verileri dışa aktarıp raporlama veya yedekleme yapmak istiyor.

Her ilgili sayfanın header'ına "Dışa Aktar" butonu eklenir:
- Users sayfası → `GET /admin/users/export?format=csv` veya `json`
- Audit logs sayfası → `GET /admin/audit-logs/export?start_date=X&end_date=X`
- Templates sayfası → `GET /templates/export?ids=X,Y,Z`
- Dosya otomatik indirilir (Content-Disposition header backend'de zaten var)

### 3.6 Bildirim Kuyruğu Yönetimi

**Kullanıcı senaryosu:** Admin, bekleyen veya başarısız bildirimlerin durumunu görmek ve müdahale etmek istiyor.

- Dashboard'daki mevcut notification stats widget'ını genişlet
- Durum kartları: Pending | Sent | Failed (sayılarla, `GET /admin/notifications/stats`)
- Tip kartları: Email | Push | In-App | Webhook | SMS
- Aksiyon butonları: "Başarısızları Tekrar Dene" (`POST /admin/notifications/retry-failed`), "Bekleyenleri İşle" (`POST /admin/notifications/process-pending`)
- Bu, ayrı bir sayfa yerine dashboard'a entegre edilebilir

---

## FAZA 4: Dashboard İyileştirmeleri

### 4.1 Admin Dashboard Güncelleme

**Mevcut:** Metric kartları + activity feed + SSE monitor + template usage

**Eklenecek:**
- "Bugünkü Kayıtlar" kartı (backend artık gerçek veri dönüyor)
- System health özet widget'ı (tek bakışta yeşil/sarı/kırmızı, tıklayınca health sayfasına gider)
- Bildirim kuyruğu widget'ı (pending/failed sayıları + aksiyon butonları)
- Email log özeti (son 24 saatte gönderilen/başarısız)

Veriler: `GET /admin/dashboard` (tek çağrıda users + notifications + sse + system)

---

## FAZA 5: SSE Entegrasyonu (Kullanılmayan Altyapıyı Devreye Alma)

### SSE Mevcut Durum

Backend 9 event tipi tanımlı, frontend sadece 2'sini dinliyor (metrics + audit_log). Geri kalanı ya backend'de emit ediliyor ama frontend dinlemiyor, ya da hiç emit edilmiyor.

### 5.1 Real-Time Bildirimler

**Mevcut sorun:** Backend in-app bildirim gönderildiğinde `notification` event'ini SSE ile yayınlıyor, ama frontend bunu dinlemiyor. Kullanıcı sayfayı yenilemeden yeni bildirimi göremiyor.

**Çözüm:**
- `useNotificationStream()` hook'u: SSE bağlantısında `notification` event'ini dinler
- Yeni bildirim geldiğinde:
  1. Sidebar'daki bildirim ikonuna kırmızı unread sayacı güncellenir
  2. Sonner toast ile anlık bildirim gösterilir (başlık + kısa mesaj)
  3. Kullanıcı bildirimler sayfasındaysa listeye canlı eklenir (tıpkı audit log'daki gibi)
- Reconnect'te `?since=lastEventTime` ile missed event'ler yakalanır (`bulk_notification`)
- Bu hook `dashboard-layout.tsx`'e (tüm authenticated sayfaları saran layout) eklenir, böylece her sayfada çalışır

### 5.2 System Message Banner

**Mevcut sorun:** `system_message` event tipi tanımlı, admin broadcast endpoint'i var (`POST /admin/sse/broadcast`), ama ne backend emit ediyor ne frontend dinliyor.

**Çözüm:**
- Backend: Admin broadcast endpoint'inin `system_message` event tipiyle yayın yapmasını sağla
- Frontend: `dashboard-layout.tsx`'e global banner componenti ekle
- Admin broadcast gönderdiğinde sayfanın üstünde sarı/kırmızı banner belirir: "Sistem bakıma alınacak — 15:00"
- Banner kapatılabilir (dismiss), ama critical mesajlar kalıcı olabilir
- Admin panelde "Sistem Mesajı Gönder" butonu: metin + seviye (info/warning/critical) + hedef (tüm kullanıcılar / belirli roller)

### 5.3 Connection Info İşleme

**Mevcut sorun:** Bağlantı kurulduğunda backend `connection_info` gönderiyor (client_id, server_version, features), frontend bunu görmezden geliyor.

**Çözüm:**
- SSE monitor widget'ında bağlantı detaylarını göster: client ID, sunucu versiyonu, bağlantı süresi
- Reconnect sayısını takip et ve göster
- Bağlantı koptuğunda/yeniden kurulduğunda toast: "Bağlantı yeniden kuruldu"

### 5.4 Dinamik Channel Subscription

**Mevcut sorun:** Backend subscribe/unsubscribe endpoint'leri var, frontend hiç kullanmıyor. Tüm event'ler tek stream'den geliyor.

**Çözüm:**
- Sayfa bazlı subscription: kullanıcı admin audit sayfasına girdiğinde `admin:audit`'e subscribe, çıktığında unsubscribe
- Template editör sayfasında: template değişiklik event'leri için subscribe (concurrent editing farkındalığı)
- Bu optimizasyon, gereksiz event trafiğini azaltır

---

## Uygulama Önceliği

| Sıra | Faz | Açıklama | Neden |
|------|-----|----------|-------|
| 1 | 1.1-1.2-1.3 | Template listesi + editör + variables | En büyük eksik, backend tamamen hazır |
| 2 | 5.1 | Real-time bildirimler (SSE) | Altyapı hazır, 0 backend değişikliği, büyük UX farkı |
| 3 | 2.1-2.2 | Rol/Permission tablo iyileştirmesi | Mevcut sayfalar var, UX upgrade |
| 4 | 3.4 | Bulk operasyonlar (users tablosuna checkbox) | Mevcut sayfaya küçük ekleme |
| 5 | 3.1 | System health sayfası | Yeni sayfa ama basit |
| 6 | 3.5 | Export butonları | Her sayfaya küçük buton ekleme |
| 7 | 1.4-1.5 | Kategori yönetimi + test email | Template editöre ek |
| 8 | 5.2 | System message banner | Admin broadcast devreye girer |
| 9 | 3.2-3.3 | Session + email log sayfaları | Yeni sayfalar |
| 10 | 3.6-4.1 | Bildirim kuyruğu + dashboard iyileştirme | Dashboard genişletme |
| 11 | 5.3-5.4 | Connection info + dinamik subscription | Optimizasyon |
| 12 | 2.3 | Matris görünümü | Nice-to-have |

---

## Teknik Notlar

- **Mevcut pattern'lere uy:** feature-based folder yapısı, shadcn/ui, React Hook Form + Zod, Zustand
- **Yeni bağımlılık önerisi:** Template editör için `@uiw/react-codemirror` veya basit textarea + syntax highlight. Ağır bir code editor (Monaco) gereksiz olabilir, Go template syntax'ı basit
- **Preview iframe:** Email HTML'i render ederken XSS riski var, `srcdoc` ile sandboxed iframe kullanılmalı
- **Bulk operasyonlar:** 207 Multi-Status response'u frontend'de partial success/failure olarak gösterilmeli
- **dnd-kit:** Projede zaten var, kategori sıralama ve variable sıralama için kullanılabilir
