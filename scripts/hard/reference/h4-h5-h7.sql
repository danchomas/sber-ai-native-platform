SET search_path=raw;
\echo ===== H4 вес и возвраты (после исправления)
SELECT min(ves_kg), max(ves_kg), round(avg(ves_kg)::numeric,2), percentile_cont(array[0.5,0.75,0.9]) within group (order by ves_kg) FROM ds_dostavka_b83b9cd9;
SELECT width_bucket(ves_kg, array[1,2,5,10,20]) b, min(ves_kg), max(ves_kg), count(*), round(100.0*avg((vozvrascheno_otpravitelyu_msk IS NOT NULL)::int),2) ret_pct, round(100.0*avg((status in ('Возвращено отправителю','Возвращается отправителю'))::int),2) ret_status, round(100.0*avg((status='Отменено')::int),2) canc FROM ds_dostavka_b83b9cd9 GROUP BY 1 ORDER BY 1;
SELECT sposob_dostavki, round(100.0*avg((vozvrascheno_otpravitelyu_msk IS NOT NULL)::int) FILTER (WHERE ves_kg>=10),2) heavy, round(100.0*avg((vozvrascheno_otpravitelyu_msk IS NOT NULL)::int) FILTER (WHERE ves_kg<2),2) light, round(100.0*avg((ves_kg>=10)::int),1) heavy_share FROM ds_dostavka_b83b9cd9 GROUP BY 1;
SELECT prichina_otmeny_ili_vozvrata, count(*) FROM ds_dostavka_b83b9cd9 WHERE vozvrascheno_otpravitelyu_msk IS NOT NULL GROUP BY 1 ORDER BY 2 DESC LIMIT 8;
\echo ===== H5 задачи: недооценка трудозатрат по типу
SELECT tip_zadachi, count(*) n, round(avg(fakticheskie_trudozatraty_ch/planovye_trudozatraty_ch)::numeric,2) avg_ratio, round((sum(fakticheskie_trudozatraty_ch)/sum(planovye_trudozatraty_ch))::numeric,2) sum_ratio, round(100.0*avg((fakticheskie_trudozatraty_ch>planovye_trudozatraty_ch)::int),1) over_pct FROM ds_zadachi_komand_68b6d258 WHERE status='Завершена' GROUP BY 1 ORDER BY 4 DESC;
SELECT planovye_trudozatraty_ch, round((sum(fakticheskie_trudozatraty_ch)/sum(planovye_trudozatraty_ch))::numeric,2) r FROM ds_zadachi_komand_68b6d258 WHERE status='Завершена' GROUP BY 1 ORDER BY 1;
\echo ===== H7 закупки: срочные vs обычные
SELECT srochnost, count(*), round(avg(extract(epoch from soglasovana_msk-podana_msk)/3600)::numeric,1) appr_h, round((percentile_cont(0.5) within group (order by extract(epoch from soglasovana_msk-podana_msk)/3600))::numeric,1) appr_med,
 count(ispolnena_msk) executed, round(100.0*avg((ispolnena_msk::date > trebuetsya_k_date)::int) FILTER (WHERE ispolnena_msk IS NOT NULL),1) late_pct,
 round(100.0*avg((status='Отклонена')::int),1) rej
FROM ds_zayavki_na_zakupku_b799b3cf GROUP BY 1;
SELECT status, count(*) FROM ds_zayavki_na_zakupku_b799b3cf GROUP BY 1 ORDER BY 2 DESC;
\echo ===== H8 фоновые: ошибки
SELECT round(100.0*avg((status='Ошибка')::int),2) err, count(*) FROM ds_fonovye_zadaniya_4497e06b;
SELECT 'env' d, okruzhenie v, count(*), round(100.0*avg((status='Ошибка')::int),2) err FROM ds_fonovye_zadaniya_4497e06b GROUP BY 2
UNION ALL SELECT 'launch', sposob_zapuska, count(*), round(100.0*avg((status='Ошибка')::int),2) FROM ds_fonovye_zadaniya_4497e06b GROUP BY 2
UNION ALL SELECT 'service', servis, count(*), round(100.0*avg((status='Ошибка')::int),2) FROM ds_fonovye_zadaniya_4497e06b GROUP BY 2
UNION ALL SELECT 'attempt', least(nomer_popytki,4)::text, count(*), round(100.0*avg((status='Ошибка')::int),2) FROM ds_fonovye_zadaniya_4497e06b GROUP BY 2
UNION ALL SELECT 'hour', lpad(extract(hour from nachalo_msk)::text,2,'0'), count(*), round(100.0*avg((status='Ошибка')::int),2) FROM ds_fonovye_zadaniya_4497e06b GROUP BY 2
UNION ALL SELECT 'dow', extract(isodow from nachalo_msk)::text, count(*), round(100.0*avg((status='Ошибка')::int),2) FROM ds_fonovye_zadaniya_4497e06b GROUP BY 2
UNION ALL SELECT 'node', uzel_vypolneniya, count(*), round(100.0*avg((status='Ошибка')::int),2) FROM ds_fonovye_zadaniya_4497e06b GROUP BY 2
UNION ALL SELECT 'size', width_bucket(zapisey_k_obrabotke, array[1000,10000,100000,1000000])::text, count(*), round(100.0*avg((status='Ошибка')::int),2) FROM ds_fonovye_zadaniya_4497e06b GROUP BY 2
ORDER BY 1, 4 DESC;
