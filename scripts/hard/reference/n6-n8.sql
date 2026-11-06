SET search_path=raw;
\echo ===== N6 лекарства: сезонность
SELECT extract(month from data) m, round(sum(skolko_kupili)/2.0) per_year FROM ds_c_09_lekarstvennye_preparaty_43f92195 GROUP BY 1 ORDER BY 1;
SELECT tip_lekarstva, round(100.0*sum(skolko_kupili) FILTER (WHERE extract(month from data) in (12,1,2)) / sum(skolko_kupili),1) winter_pct, round(100.0*sum(skolko_kupili) FILTER (WHERE extract(month from data) in (6,7,8)) / sum(skolko_kupili),1) summer_pct, round(100.0*sum(skolko_kupili) FILTER (WHERE extract(month from data) in (3,4,5)) / sum(skolko_kupili),1) spring_pct, sum(skolko_kupili) total FROM ds_c_09_lekarstvennye_preparaty_43f92195 GROUP BY 1 ORDER BY 2 DESC;
\echo ===== N7 фоновые: объём и ошибки
SELECT percentile_cont(array[0.25,0.5,0.75,0.9]) within group (order by zapisey_k_obrabotke) FROM ds_fonovye_zadaniya_4497e06b;
SELECT ntile, min(zapisey_k_obrabotke), max(zapisey_k_obrabotke), count(*), round(100.0*avg((status='Ошибка')::int),2) err FROM (SELECT *, ntile(5) over (order by zapisey_k_obrabotke) FROM ds_fonovye_zadaniya_4497e06b) x GROUP BY 1 ORDER BY 1;
SELECT okruzhenie, ntile, round(100.0*avg((status='Ошибка')::int),2) err FROM (SELECT *, ntile(3) over (order by zapisey_k_obrabotke) FROM ds_fonovye_zadaniya_4497e06b) x GROUP BY 1,2 ORDER BY 1,2;
SELECT opisanie_oshibki, round(avg(zapisey_k_obrabotke)) avg_rec, count(*) FROM ds_fonovye_zadaniya_4497e06b WHERE status='Ошибка' GROUP BY 1 ORDER BY 3 DESC;
\echo ===== N8 реклама: стоимость заказа
SELECT 'ploschadka' k, ploschadka v, round((sum(rashody_rub)/nullif(sum(zakazy_po_reklame),0))::numeric) cpo FROM ds_reklamnye_kampanii_daedfb94 GROUP BY 2
UNION ALL SELECT 'auditoriya', auditoriya, round((sum(rashody_rub)/nullif(sum(zakazy_po_reklame),0))::numeric) FROM ds_reklamnye_kampanii_daedfb94 GROUP BY 2
UNION ALL SELECT 'kategoriya', kategoriya_tovarov, round((sum(rashody_rub)/nullif(sum(zakazy_po_reklame),0))::numeric) FROM ds_reklamnye_kampanii_daedfb94 GROUP BY 2
UNION ALL SELECT 'month', extract(month from data)::text, round((sum(rashody_rub)/nullif(sum(zakazy_po_reklame),0))::numeric) FROM ds_reklamnye_kampanii_daedfb94 GROUP BY 2
UNION ALL SELECT 'dow', extract(isodow from data)::text, round((sum(rashody_rub)/nullif(sum(zakazy_po_reklame),0))::numeric) FROM ds_reklamnye_kampanii_daedfb94 GROUP BY 2
ORDER BY 1, 3;
SELECT round((sum(rashody_rub)/sum(zakazy_po_reklame))::numeric) cpo_all FROM ds_reklamnye_kampanii_daedfb94;
