\echo === по объёму
select case when zapisey_k_obrabotke >= 100000 then '3 >=100k' when zapisey_k_obrabotke >= 10000 then '2 10k-100k' else '1 <10k' end b,
  count(*), round(100.0*avg((status='Ошибка')::int),2) err from raw.ds_fonovye_zadaniya_4497e06b group by 1 order by 1;
\echo === по окружению
select okruzhenie, count(*), round(100.0*avg((status='Ошибка')::int),2) err, round(avg(zapisey_k_obrabotke)) avg_vol from raw.ds_fonovye_zadaniya_4497e06b group by 1 order by 1;
\echo === объём внутри окружения
select okruzhenie, case when zapisey_k_obrabotke >= 100000 then '3 >=100k' when zapisey_k_obrabotke >= 10000 then '2 10k-100k' else '1 <10k' end b,
  count(*), round(100.0*avg((status='Ошибка')::int),2) err from raw.ds_fonovye_zadaniya_4497e06b group by 1,2 order by 1,2;
\echo === статусы
select status, count(*) from raw.ds_fonovye_zadaniya_4497e06b group by 1;
