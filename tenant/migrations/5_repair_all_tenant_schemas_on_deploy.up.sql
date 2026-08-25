-- Jalankan otomatis saat deploy Encore (sebelum dynamic grants) agar owner t_* + GRANT
-- sudah benar tanpa script shell lokal.
DO $$
DECLARE
  s text;
BEGIN
  IF to_regprocedure('public.repair_tenant_schema_grants(text)') IS NULL THEN
    RAISE NOTICE 'repair_tenant_schema_grants(text) belum ada — skip deploy repair';
    RETURN;
  END IF;

  FOR s IN
    SELECT nspname FROM pg_namespace WHERE nspname ~ '^t_' ORDER BY 1
  LOOP
    BEGIN
      PERFORM public.repair_tenant_schema_grants(s);
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'deploy repair skip %: %', s, SQLERRM;
    END;
  END LOOP;
END $$;
