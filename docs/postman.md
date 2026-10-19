# EduPilot API Documentation (Postman)

This documentation provides the list of endpoints available through the `edupilot-gateway` (running on `http://localhost:8888`). You can use this to create collections in Postman.

## Base URL
`{{base_url}}` = `http://localhost:8888`

---

## 1. Auth Service (`/auth`, `/customer`, `/other`)

### Public Authentication
- **Register**
  - `POST {{base_url}}/auth/register`
  - Body: JSON (User registration details)
- **Login**
  - `POST {{base_url}}/auth/login`
  - Body: JSON (`email`, `password`)
- **Refresh Token**
  - `POST {{base_url}}/auth/refresh`
  - Body: JSON (`refresh_token`)
- **Forgot Password**
  - `POST {{base_url}}/auth/forgot-password`
  - Body: JSON (`email`)
- **Reset Password**
  - `POST {{base_url}}/auth/reset-password`
  - Body: JSON (`token`, `new_password`)
- **Google Login (Redirect)**
  - `GET {{base_url}}/auth/google/login`
- **Google Callback**
  - `GET {{base_url}}/auth/google/callback`

### Protected Authentication (Requires Bearer Token)
- **Get Current User (Me)**
  - `GET {{base_url}}/auth/me`
  - Headers: `Authorization: Bearer {{token}}`
- **Verify Email**
  - `POST {{base_url}}/auth/verify`
- **Resend OTP**
  - `POST {{base_url}}/auth/resend-otp`
- **Update Profile**
  - `PUT {{base_url}}/auth/profile`
- **Update Profile By ID**
  - `PUT {{base_url}}/auth/profile/:id`
- **Change Role**
  - `PUT {{base_url}}/auth/change-role`
- **Update Photo Profile**
  - `PUT {{base_url}}/auth/photo-profile`
  - Body: `multipart/form-data`
- **Logout**
  - `POST {{base_url}}/auth/logout`

### Other Management (`/other`)
- **Get Statistics**
  - `GET {{base_url}}/other/statistik`
- **Get All Customer Users**
  - `GET {{base_url}}/other/customers`
- **Get Customer User Detail**
  - `GET {{base_url}}/other/customers/:customer_id`

---

## 2. Instantion Service (`/instantion` or Root depending on Gateway prefix setup)

*Note: Since the gateway proxies `/instantion` directly to the instantion-service, if the instantion service expects the root path, you might need to use these paths, or update the gateway to strip the `/instantion` prefix.*

- **Health Check**
  - `GET {{base_url}}/health` (or `GET {{base_url}}/instantion/health`)
- **Swagger Documentation**
  - `GET {{base_url}}/docs/index.html` (or `GET {{base_url}}/instantion/docs/index.html`)

---

## How to test in Postman:
1. Create a new Environment in Postman.
2. Add a variable named `base_url` with the initial value `http://localhost:8888`.
3. Add a variable named `token` (leave it blank initially, you can fill it after Login).
4. Create requests matching the above methods and URLs.
5. For protected routes, go to the **Authorization** tab, select **Bearer Token**, and use `{{token}}`.
