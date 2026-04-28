# Project Accounting Design

## หลักการ

- `projects` เป็นมิติวิเคราะห์ของผู้ใช้แต่ละ LINE ID แยกด้วย `userId + namespace`
- `transactions` ยังเป็นรายการบัญชีหลัก และยังผูกกับ double-entry ledger ผ่าน `journal_entries/journal_lines`
- `transaction_project_allocations` เป็น many-to-many ระหว่างรายการบัญชีกับโครงการ
- allocation รวมกันได้ไม่เกินยอดรายการ แต่ไม่จำเป็นต้องครบ 100% เพื่อรองรับรายการที่ยังไม่ได้จัดสรรทั้งหมด
- การลบ transaction เป็น soft delete (`deletedAt`) และ void journal เดิม จึงไม่ถูกคำนวณต่อ แต่ยังมี audit/event history

## Tables

- `projects`
  - ชื่อโครงการ, สี, สถานะ, soft delete
  - unique ชื่อภายใน namespace เฉพาะโครงการที่ยังไม่ถูกลบ
- `transaction_project_allocations`
  - `transactionId + projectId` unique
  - เก็บ `amount` และ `percent` ต่อรายการ
- `transactions`
  - เพิ่ม `counterpartyName`, `counterpartyRole`, `updatedAt`, `deletedAt`

## Flow

1. ผู้ใช้เปิด Mini App ผ่าน signed token เฉพาะ LINE ID
2. เพิ่ม/แก้ชื่อโครงการในหน้า LIFF
3. เปิดรายการบัญชีแล้วแก้จำนวน หมวด รายละเอียด วันเวลา รับจาก/จ่ายให้
4. ใส่ยอด allocation เข้าได้หลายโครงการ
5. ระบบ validate owner/namespace ทุกครั้ง
6. เมื่อแก้รายการ ระบบ void journal เดิมแล้วสร้าง journal ใหม่
7. ทุก mutation บันทึก `ledger_events` และ Mongo event log

## Safety

- ทุก query filter ด้วย `userId + namespace`
- ห้ามแก้/ลบ project หรือ transaction ข้าม LINE ID
- Dashboard ไม่รวม transaction ที่ `deletedAt IS NOT NULL`
- Ledger check นับเฉพาะ journal `status = POSTED`
