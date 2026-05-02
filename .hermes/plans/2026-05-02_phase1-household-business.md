# Phase 1 Plan: Household + Business Ledger, LIFF Production

**Created:** 2026-05-02 10:45  
**Branch:** `phase1-household-business-ledger`  
**Goal:** เติม Household & Business ledger ให้ใช้งานได้จริง + LIFF production ready

---

## Current State

### มีแล้ว ✅
- Personal ledger — parser, writer, journal, summary, dashboard (Go)
- DB schema: `households`, `household_members`, `businesses` (PostgreSQL)
- Prisma models: Household, HouseholdMember, Business (TypeScript)
- LIFF static HTML — dashboard UI 1214 บรรทัด (Go embedded)
- Dispatcher — routes LINE text to parser → ledger
- SMLGateway connected via `sml/fast`

### ขาด ❌
- Go `household/` package — no CRUD, no member management
- Go `business/` package — no sales/purchases tracking
- Dispatcher doesn't route household/business intents
- LIFF doesn't show household/business views
- No tests for household/business

---

## Approach

**Go-first strategy**: สร้างทุกอย่างใน Go core ก่อน (ตามแผน migration TypeScript → Go)  
**TDD**: เขียน tests ก่อน implementation ทุก package  
**Reuse**: ใช้ `ledger.Writer` เดิม — household/business แค่เพิ่ม scope filter

---

## Step-by-Step Plan

### Step 1: Household Ledger Package
**Files:**
- `go-core/internal/household/household.go` — CreateHousehold, AddMember, RemoveMember, ListMembers
- `go-core/internal/household/household_test.go` — unit tests
- `go-core/internal/household/summary.go` — GetHouseholdSummary (reuses ledger.Writer with household scope)
- `go-core/internal/household/handler.go` — HTTP handler สำหรับ API

**DB Operations:**
```sql
INSERT INTO households (id, name, "ownerId") VALUES (...)
INSERT INTO household_members ("householdId", "userId", role) VALUES (...)
SELECT * FROM household_members WHERE "householdId" = $1
```

**Summary:** ใช้ `ledger.Writer.FetchUserRange()` แต่เปลี่ยน scope filter เป็น `scope = 'HOUSEHOLD' AND "scopeId" = householdId`

---

### Step 2: Business Ledger Package
**Files:**
- `go-core/internal/business/business.go` — CreateBusiness, RecordSale, RecordPurchase
- `go-core/internal/business/business_test.go`
- `go-core/internal/business/profit.go` — GetProfitSummary, GetCostBreakdown
- `go-core/internal/business/handler.go`

**Reuse:** `ledger.Writer.CreatePersonalTransaction()` — แต่เปลี่ยน scope + scopeId
- Business sales: `type=INCOME, scope=BUSINESS, scopeId=businessId`
- Business purchases: `type=EXPENSE, scope=BUSINESS, scopeId=businessId`

```
Profit = SUM(INCOME) - SUM(EXPENSE) WHERE scope='BUSINESS' AND scopeId=?
```

---

### Step 3: Update Dispatcher
**File:** `go-core/internal/dispatcher/dispatcher.go`

Add intent routing:
- `"สร้างบ้าน"`, `"ครัวเรือน"` → household flow  
- `"ร้าน"`, `"ธุรกิจ"`, `"ขาย"` → business flow
- Auto-detect scope from previous session context

Parser integration:
- `parser.Parse()` already handles `ขายของ 1500` as income → extend to tag scope

---

### Step 4: Update LIFF Dashboard
**File:** `go-core/internal/liff/static.go`

Split into tabs/screens:
- **Tab 1:** ส่วนตัว (existing)
- **Tab 2:** ครัวเรือน — สมาชิก, งบรวม, กราฟ
- **Tab 3:** ธุรกิจ — ยอดขาย, ต้นทุน, กำไร

Or: single scroll page with sections + sticky tabs

---

### Step 5: API Endpoints
Add to `cmd/api/main.go`:
```go
r.Route("/household", func(r chi.Router) {
    r.Post("/", householdHandler.Create)
    r.Post("/{id}/members", householdHandler.AddMember)
    r.Delete("/{id}/members/{userId}", householdHandler.RemoveMember)
    r.Get("/{id}/summary", householdHandler.Summary)
})

r.Route("/business", func(r chi.Router) {
    r.Post("/", businessHandler.Create)
    r.Post("/{id}/sale", businessHandler.RecordSale)
    r.Post("/{id}/purchase", businessHandler.RecordPurchase)
    r.Get("/{id}/profit", businessHandler.Profit)
})
```

---

### Step 6: Tests & Verification
- [ ] Go tests: `go test ./internal/household/...`
- [ ] Go tests: `go test ./internal/business/...`
- [ ] Integration: create household → add 2 members → record expense → summary shows correctly
- [ ] Integration: create business → record 3 sales + 2 purchases → profit correct
- [ ] All existing tests still pass (28 Node.js + Go parser/ledger)
- [ ] API builds: `go build ./cmd/api`

---

### Step 7: LINE OA Production Config (Manual — นพต้องทำ)
⚠️ **ต้องใช้ LINE Developers console:**
1. เปิด LINE OA → Messaging API channel
2. ตั้ง Webhook URL: `https://your-domain.com/webhook/line`
3. เปิด LIFF app → set URL to `https://your-domain.com/liff/dashboard`
4. Copy `CHANNEL_SECRET` + `CHANNEL_ACCESS_TOKEN` → ใส่ใน `.env`

---

### Step 8: Git Workflow
```bash
# After each step:
git add -A
git commit -m "household: add CreateHousehold + AddMember (#phase1)"
git push origin phase1-household-business-ledger

# When all done → PR to main
```

---

## Files Summary

| Package | New Files | Modified Files |
|---------|-----------|----------------|
| `household/` | 3 files | — |
| `business/` | 3 files | — |
| `dispatcher/` | — | dispatcher.go (add routes) |
| `liff/` | — | static.go (add tabs) |
| `cmd/api/` | — | main.go (add handlers) |
| `migrations/` | 1 file | — (005_household_business.sql) |

---

## Risks & Tradeoffs

| Risk | Mitigation |
|------|------------|
| Household members ต่าง LINE ID → ต้อง share namespace | ใช้ invitation flow (ส่ง link แชร์ผ่าน LINE) |
| Business อาจต้องมี inventory → scope creep | MVP: income/expense tracking only |
| LIFF static.go 1214 บรรทัด — แก้ยาก | พิจารณาแยกเป็น template files ใน Phase 2 |
| LINE OA production requires real channel | นพต้องตั้งเอง — ภูผาเตรียม config ให้ |

---

## Success Criteria

- ✅ สร้าง household ได้ → เพิ่มสมาชิก → จดรายจ่ายครัวเรือน → ดูสรุปได้
- ✅ สร้าง business ได้ → บันทึกยอดขาย/ต้นทุน → ดูกำไรได้
- ✅ LIFF แสดงทั้ง personal + household + business
- ✅ Tests ผ่านหมด (Go + Node.js)
- ✅ API build ไม่พัง
