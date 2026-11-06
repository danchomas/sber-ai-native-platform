SET search_path=raw;
\echo ===== H1 поддержка: нарушение норматива (закрытые)
CREATE TEMP TABLE sp AS SELECT *, extract(epoch from zakryto_msk-sozdano_msk)/3600 res_h FROM ds_tehnicheskaya_podderzhka_04cc29d7;
SELECT round(100.0*avg((res_h>normativ_resheniya_ch)::int),1) late_pct, count(*) FILTER (WHERE res_h IS NOT NULL) closed, count(*) FROM sp;
SELECT 'prioritet' d, prioritet v, count(*) n, round(100.0*avg((res_h>normativ_resheniya_ch)::int),1) late FROM sp WHERE res_h IS NOT NULL GROUP BY 2
UNION ALL SELECT 'kanal', kanal_obrascheniya, count(*), round(100.0*avg((res_h>normativ_resheniya_ch)::int),1) FROM sp WHERE res_h IS NOT NULL GROUP BY 2
UNION ALL SELECT 'gruppa', gruppa_podderzhki, count(*), round(100.0*avg((res_h>normativ_resheniya_ch)::int),1) FROM sp WHERE res_h IS NOT NULL GROUP BY 2
UNION ALL SELECT 'tema', tema, count(*), round(100.0*avg((res_h>normativ_resheniya_ch)::int),1) FROM sp WHERE res_h IS NOT NULL GROUP BY 2
UNION ALL SELECT 'month', extract(month from sozdano_msk)::text, count(*), round(100.0*avg((res_h>normativ_resheniya_ch)::int),1) FROM sp WHERE res_h IS NOT NULL GROUP BY 2
UNION ALL SELECT 'dow', extract(isodow from sozdano_msk)::text, count(*), round(100.0*avg((res_h>normativ_resheniya_ch)::int),1) FROM sp WHERE res_h IS NOT NULL GROUP BY 2
UNION ALL SELECT 'hour', lpad(extract(hour from sozdano_msk)::text,2,'0'), count(*), round(100.0*avg((res_h>normativ_resheniya_ch)::int),1) FROM sp WHERE res_h IS NOT NULL GROUP BY 2
ORDER BY 1, 4 DESC;
\echo ===== H2 повторные открытия vs оценка, с контролем длительности
SELECT least(povtornye_otkrytiya,3) reopen, count(*), count(ocenka_klienta) rated, round(avg(ocenka_klienta),2) score, round(avg(res_h)::numeric,1) res_h FROM sp GROUP BY 1 ORDER BY 1;
SELECT CASE WHEN res_h<24 THEN '<24' WHEN res_h<72 THEN '24-72' ELSE '72+' END bucket, (povtornye_otkrytiya>0) reopened, count(ocenka_klienta) n, round(avg(ocenka_klienta),2) score FROM sp WHERE res_h IS NOT NULL GROUP BY 1,2 ORDER BY 1,2;
