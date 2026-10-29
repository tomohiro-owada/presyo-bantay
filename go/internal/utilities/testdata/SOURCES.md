# Fixture provenance

All files were fetched unmodified on 2026-10-26 between 06:50 and 07:00
Asia/Manila (UTC+8) with the PresyoBantay UA, except
`maynilad_maintenance_posts.json`, which is a 2-post subset (2026-10-23 and
2026-08-21) of the WordPress REST response. Do not edit; tests depend on them.

- DOE listing pages: https://doe.gov.ph/data-and-prices/liquid-fuels/oil-monitor ,
  .../liquid-fuels/retail-pump-prices/ncr-pump-prices , .../lpg-monitor/ncr-lpg-prices ,
  .../liquid-fuels/retail-pump-prices/price-adjustments
- DOE PDFs: https://d24qbtp4vooyzi.cloudfront.net/api/media/file/<name>.pdf?prefix=dev%2Fmedia
  (Oil Monitor 15 Sep – 20 Oct 2026; NCR Price Monitoring 10062026 / 10132026;
  NCR September/October 2026 LPG Price Monitoring; Price Adjustment on Fuel
  20-26 Oct 2026; LPG Price Adjustment As of October 2026)
- Meralco: https://company.meralco.com.ph/news-and-advisories/latest-news-and-press-releases ,
  .../higher-residential-rates-october-2026 , .../lower-rates-september-2026
- Meralco maintenance schedule (Part B/C, captured 2026-10-29):
  https://company.meralco.com.ph/news-and-advisories/maintenance-schedule
  (`?field_service_maintenance_loc_target_id=215|223|221|213&page=N`);
  fixtures `meralco_ms/all_p0..p5.html`, `city213/215/221/223_p0..p2.html`,
  `layout_changed.html` (trimmed view rows + pager only). Do not edit.
- Maynilad: https://www.mayniladwater.com.ph/wp-json/interruptions/{scheduled,emergency}?id=&city=... ,
  https://www.mayniladwater.com.ph/wp-json/wp/v2/posts?search=maintenance%20activities
- Maynilad weekly-schedule RSS feed (Part A/C, captured 2026-10-29):
  https://www.mayniladwater.com.ph/feed/ (`?paged=2` only when page 1 has
  no weekly post); fixtures `maynilad_feed/feed_p1..p8.xml`. Do not edit;
  add new fixtures only as new files.
