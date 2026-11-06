SET search_path=raw;
\echo ===== N2 на экране Оплата
SELECT 'platforma' d, platforma v, count(*), round(100.0*avg((tekst_oshibki is not null)::int),2) e FROM ds_sobytiya_prilozheniya_1dbbf453 WHERE ekran='Оплата' GROUP BY 2
UNION ALL SELECT 'sobytie', sobytie, count(*), round(100.0*avg((tekst_oshibki is not null)::int),2) FROM ds_sobytiya_prilozheniya_1dbbf453 WHERE ekran='Оплата' GROUP BY 2
UNION ALL SELECT 'istochnik', istochnik_sessii, count(*), round(100.0*avg((tekst_oshibki is not null)::int),2) FROM ds_sobytiya_prilozheniya_1dbbf453 WHERE ekran='Оплата' GROUP BY 2
UNION ALL SELECT 'month', extract(month from vremya_sobytiya_msk)::text, count(*), round(100.0*avg((tekst_oshibki is not null)::int),2) FROM ds_sobytiya_prilozheniya_1dbbf453 WHERE ekran='Оплата' GROUP BY 2
ORDER BY 1, 4 DESC;
SELECT tekst_oshibki, count(*) FROM ds_sobytiya_prilozheniya_1dbbf453 WHERE tekst_oshibki IS NOT NULL GROUP BY 1 ORDER BY 2 DESC LIMIT 6;
\echo ===== N3 доставка: передано -> доставлено
CREATE TEMP TABLE d AS SELECT *, extract(epoch from dostavleno_msk-peredano_perevozchiku_msk)/3600 h FROM ds_dostavka_b83b9cd9;
SELECT round(avg(h)::numeric,1) avg_h, percentile_cont(0.5) within group (order by h) med FROM d;
SELECT 'perevozchik' k, perevozchik v, count(h), round(avg(h)::numeric,1) avg_h FROM d GROUP BY 2
UNION ALL SELECT 'sposob', sposob_dostavki, count(h), round(avg(h)::numeric,1) FROM d GROUP BY 2
UNION ALL SELECT 'month', extract(month from prinyato_msk)::text, count(h), round(avg(h)::numeric,1) FROM d GROUP BY 2
UNION ALL SELECT 'dow', extract(isodow from peredano_perevozchiku_msk)::text, count(h), round(avg(h)::numeric,1) FROM d GROUP BY 2
UNION ALL SELECT 'same_city', (gorod_otpravleniya=gorod_polucheniya)::text, count(h), round(avg(h)::numeric,1) FROM d GROUP BY 2
UNION ALL SELECT 'attempts', least(popytki_vrucheniya,3)::text, count(h), round(avg(h)::numeric,1) FROM d GROUP BY 2
ORDER BY 1, 4 DESC;
SELECT region_polucheniya, count(h), round(avg(h)::numeric,1) avg_h FROM d GROUP BY 1 ORDER BY 3 DESC LIMIT 5;
SELECT region_polucheniya, count(h), round(avg(h)::numeric,1) avg_h FROM d GROUP BY 1 ORDER BY 3 LIMIT 3;
