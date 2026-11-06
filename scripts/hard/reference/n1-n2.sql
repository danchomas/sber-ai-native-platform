SET search_path=raw;
\echo ===== N1 версии приложения: время обработки
SELECT versiya_prilozheniya v, min(vremya_sobytiya_msk)::date first_seen, count(*), round(avg(vremya_obrabotki_ms)) avg_ms, percentile_cont(0.5) within group (order by vremya_obrabotki_ms) med_ms, round(100.0*avg((tekst_oshibki is not null)::int),2) err FROM ds_sobytiya_prilozheniya_1dbbf453 GROUP BY 1 ORDER BY 2;
SELECT platforma, versiya_prilozheniya, count(*), round(avg(vremya_obrabotki_ms)) avg_ms FROM ds_sobytiya_prilozheniya_1dbbf453 WHERE vremya_sobytiya_msk >= '2026-04-01' GROUP BY 1,2 ORDER BY 1,2;
\echo ===== N2 ошибки в приложении
SELECT round(100.0*avg((tekst_oshibki is not null)::int),2) err FROM ds_sobytiya_prilozheniya_1dbbf453;
SELECT 'platforma' d, platforma v, count(*), round(100.0*avg((tekst_oshibki is not null)::int),2) e FROM ds_sobytiya_prilozheniya_1dbbf453 GROUP BY 2
UNION ALL SELECT 'sobytie', sobytie, count(*), round(100.0*avg((tekst_oshibki is not null)::int),2) FROM ds_sobytiya_prilozheniya_1dbbf453 GROUP BY 2
UNION ALL SELECT 'istochnik', istochnik_sessii, count(*), round(100.0*avg((tekst_oshibki is not null)::int),2) FROM ds_sobytiya_prilozheniya_1dbbf453 GROUP BY 2
UNION ALL SELECT 'ekran', ekran, count(*), round(100.0*avg((tekst_oshibki is not null)::int),2) FROM ds_sobytiya_prilozheniya_1dbbf453 GROUP BY 2
UNION ALL SELECT 'hour', lpad(extract(hour from vremya_sobytiya_msk)::text,2,'0'), count(*), round(100.0*avg((tekst_oshibki is not null)::int),2) FROM ds_sobytiya_prilozheniya_1dbbf453 GROUP BY 2
ORDER BY 1, 4 DESC;
