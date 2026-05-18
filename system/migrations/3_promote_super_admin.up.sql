-- Dev/staging: promote platform super admin (run after account exists).
UPDATE tenant_account
SET role = 'super_admin', updated_at = NOW()
WHERE lower(email) = 'superadmin@gmail.com'
  AND deleted_at IS NULL;
