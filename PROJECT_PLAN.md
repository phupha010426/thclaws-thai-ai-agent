# แผนสร้าง ThaiAiAgent รุ่นแรก

## เป้าหมาย MVP

- ใช้ผ่าน LINE OA
- คนทั่วไปจดบัญชีส่วนตัวได้
- บ้านจดบัญชีครัวเรือนได้
- ร้านเล็กเริ่มจดยอดขาย/ต้นทุนได้
- มีความจำระยะยาวแบบ text
- ออกแบบรองรับ thClaws และ MCP

## 7 วันแรก

1. เปิด LINE OA + Messaging API
2. ตั้ง webhook `/webhook/line`
3. รับ text แล้ว queue เข้า worker
4. parser ข้อความเงินภาษาไทย
5. บันทึก transaction ลง PostgreSQL
6. จำข้อความสำคัญลง MongoDB
7. ตอบกลับ LINE แบบ reply-first

## 30 วัน

- LIFF dashboard
- Household member/permission
- Business ledger
- รายงานร้านก๋วยเตี๋ยว
- Rich menu
- Consent page

## 90 วัน

- Cooperative buying pilot
- Supplier quote
- Local logistics
- Cost dashboard
- MCP Gateway
