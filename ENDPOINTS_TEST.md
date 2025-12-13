# API Endpoints Test Checklist

## Authentication Endpoints

### 1. Register
- [ ] POST `/api/v1/auth/register`
- [ ] Verify user created in database
- [ ] Verify verification email sent to MailHog

### 2. Login
- [ ] POST `/api/v1/auth/login`
- [ ] Verify access_token returned
- [ ] Verify refresh_token returned
- [ ] Test with valid credentials
- [ ] Test with invalid credentials

### 3. Verify Email
- [ ] GET `/api/v1/auth/verify-email?token=<token>`
- [ ] Verify user.verified field updated to true
- [ ] Verify user.status changes if needed

### 4. Resend Verification Email
- [ ] POST `/api/v1/auth/resend-verification`
- [ ] Verify new verification email sent to MailHog

### 5. Logout
- [ ] POST `/api/v1/auth/logout`
- [ ] Verify response

### 6. Refresh Token
- [ ] POST `/api/v1/auth/refresh`
- [ ] Verify new access_token returned
- [ ] Test with valid refresh token
- [ ] Test with invalid refresh token

### 7. Request Password Reset
- [ ] POST `/api/v1/auth/request-password-reset`
- [ ] Verify password reset email sent to MailHog

### 8. Reset Password
- [ ] POST `/api/v1/auth/reset-password`
- [ ] Verify password changed in database

### 9. Change Password
- [ ] POST `/api/v1/auth/change-password`
- [ ] Verify password changed while authenticated

## User Profile Endpoints

### 10. Get Profile
- [ ] GET `/api/v1/users/profile`
- [ ] Verify requires authentication
- [ ] Verify returns correct user data

## Admin Endpoints

### 11. List All Users
- [ ] GET `/api/v1/admin/users?page=1&limit=10`
- [ ] Verify requires admin role
- [ ] Verify pagination works

## Notification Endpoints

### 12. Get Notifications
- [ ] GET `/api/v1/notifications?page=1&limit=20`
- [ ] Verify requires authentication

### 13. Mark Notification as Read
- [ ] PUT `/api/v1/notifications/:id/read`
- [ ] Verify requires authentication

### 14. Get Notification Preferences
- [ ] GET `/api/v1/notifications/preferences`
- [ ] Verify requires authentication

### 15. Update Notification Preferences
- [ ] PUT `/api/v1/notifications/preferences`
- [ ] Verify preferences updated

## Health & Docs Endpoints

### 16. Liveness Probe
- [ ] GET `/livez`
- [ ] Verify returns 200 with status ok

### 17. Readiness Probe
- [ ] GET `/readyz`
- [ ] Verify returns 200 with status ready
- [ ] Verify checks all services

### 18. Metrics
- [ ] GET `/metrics`
- [ ] Verify Prometheus metrics returned

### 19. API Documentation
- [ ] GET `/docs`
- [ ] Verify Swagger UI loads
- [ ] Verify all endpoints documented

## Test Results

### ✅ Completed
- [x] 1. Register - User created successfully
- [x] 2. Login - Access token and refresh token returned
- [x] 3. Verify Email - GET with query parameter works
- [x] 6. Refresh Token - Returns new access/refresh tokens
- [x] 10. Get Profile - Authentication works, returns user data
- [x] 11. Admin Endpoint - Role-based access control works (403 for non-admin)
- [x] 12. Get Notifications - Endpoint works, returns empty list
- [x] 14. Get Notification Preferences - Returns preferences with defaults
- [x] 16. Liveness Probe - /livez returns 200 ok
- [x] 17. Readiness Probe - /readyz returns 200 with all services healthy
- [x] 19. API Documentation - /docs Swagger UI loads

### ⚠️ Issues Found
- [ ] 18. Metrics - /metrics endpoint returns 500 error (needs investigation)

### 🔄 Not Yet Tested (or todo endpoints)
- [ ] 4. Resend Verification Email
- [ ] 5. Logout
- [ ] 7. Request Password Reset
- [ ] 8. Reset Password
- [ ] 9. Change Password
- [ ] 13. Mark Notification as Read
- [ ] 15. Update Notification Preferences

## Notes
- Base URL: http://localhost:3000
- MailHog UI: http://localhost:8025
- Prometheus: http://localhost:9090
- Grafana: http://localhost:3001
