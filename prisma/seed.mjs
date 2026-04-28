import { PrismaClient } from '@prisma/client';

const prisma = new PrismaClient();

const categories = [
  { type: 'EXPENSE', name: 'อาหารและเครื่องดื่ม' },
  { type: 'EXPENSE', name: 'ค่าน้ำค่าไฟ' },
  { type: 'EXPENSE', name: 'เดินทาง' },
  { type: 'EXPENSE', name: 'ของใช้เข้าบ้าน' },
  { type: 'EXPENSE', name: 'การศึกษา' },
  { type: 'EXPENSE', name: 'สุขภาพ' },
  { type: 'EXPENSE', name: 'อื่น ๆ' },
  { type: 'INCOME', name: 'เงินเดือน' },
  { type: 'INCOME', name: 'รายได้จากการขาย' },
  { type: 'INCOME', name: 'รายรับอื่น ๆ' }
];

for (const category of categories) {
  await prisma.category.upsert({
    where: {
      scope_type_name: {
        scope: 'PERSONAL',
        type: category.type,
        name: category.name
      }
    },
    update: {},
    create: {
      scope: 'PERSONAL',
      type: category.type,
      name: category.name,
      isDefault: true
    }
  });
}

await prisma.$disconnect();
