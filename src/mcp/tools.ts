export const mcpTools = [
  {
    name: 'create_personal_transaction',
    description: 'บันทึกรายรับรายจ่ายส่วนตัวของผู้ใช้ตามสิทธิ์',
    requiredScope: 'ledger:personal:write'
  },
  {
    name: 'get_personal_summary',
    description: 'ดูสรุปบัญชีส่วนตัวตามสิทธิ์',
    requiredScope: 'ledger:personal:read_summary'
  },
  {
    name: 'create_household_transaction',
    description: 'บันทึกรายรับรายจ่ายครัวเรือนตามสิทธิ์',
    requiredScope: 'ledger:household:write'
  },
  {
    name: 'get_household_summary',
    description: 'ดูสรุปบัญชีครัวเรือนตามสิทธิ์และ consent',
    requiredScope: 'ledger:household:read_summary'
  },
  {
    name: 'create_business_sale',
    description: 'บันทึกยอดขายร้านค้าเล็ก เช่น ร้านก๋วยเตี๋ยว',
    requiredScope: 'ledger:business:write'
  },
  {
    name: 'record_business_purchase',
    description: 'บันทึกต้นทุนหรือรายการซื้อของร้านค้า',
    requiredScope: 'ledger:business:write'
  },
  {
    name: 'get_business_profit_summary',
    description: 'ดูสรุปกำไรขาดทุนร้านค้า',
    requiredScope: 'ledger:business:read_summary'
  },
  {
    name: 'request_coop_group_buy',
    description: 'รวมความต้องการซื้อวัตถุดิบแบบไม่เปิดเผยข้อมูลรายร้าน',
    requiredScope: 'coop:group_buy:request'
  },
  {
    name: 'create_agent_program',
    description: 'ให้ thClaws สร้างโปรแกรมส่วนตัวแบบ sandbox draft แยกตาม namespace ผู้ใช้',
    requiredScope: 'agent:program:write'
  },
  {
    name: 'list_agent_programs',
    description: 'ดูรายการโปรแกรมส่วนตัวของ agent ใน namespace ปัจจุบันเท่านั้น',
    requiredScope: 'agent:program:read'
  }
] as const;
