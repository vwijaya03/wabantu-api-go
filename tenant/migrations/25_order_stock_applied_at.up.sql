-- order.stock_applied_at: per-order stock marker (set while the order holds issued stock,
-- NULL after a full restore). POST /orders/:id/approve claims it atomically
-- (UPDATE … WHERE stock_applied_at IS NULL RETURNING) so stock is deducted at most once;
-- inventory.SyncOrderStock maintains it under a row lock for every other path.
-- Numbered 25 per the branch rule (AI/H1 migrations start at 25); 6–24 are intentionally unused.
-- Tenant tables live in t_* schemas, so add the column to each existing schema; new tenants get
-- it from the tenant bootstrap DDL (tenant/tenant.go). Code tolerates the column being absent.
DO $$
DECLARE
  s text;
BEGIN
  FOR s IN
    SELECT n.nspname FROM pg_namespace n
    JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = 'order' AND c.relkind = 'r'
    WHERE n.nspname ~ '^t_' ORDER BY 1
  LOOP
    BEGIN
      EXECUTE format('ALTER TABLE %I."order" ADD COLUMN IF NOT EXISTS stock_applied_at TIMESTAMPTZ', s);
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'stock_applied_at skip %: %', s, SQLERRM;
    END;
  END LOOP;
END $$;
