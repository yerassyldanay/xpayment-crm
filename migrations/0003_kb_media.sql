-- 0003_kb_media.sql — topic-owned knowledge base wired to the real, git-tracked
-- media in /media/kb_* (see media/ dir). Runs exactly once (schema_migrations).
-- Reconciles prices to the current tariff infographics and replaces the old
-- phantom asset rows with the real files.

-- 1. Topics gain free-text keywords (shown to the model to help topic/media match).
ALTER TABLE kb_topics ADD COLUMN keywords TEXT NOT NULL DEFAULT '';

-- 2. Price book = the new published tariffs (Пробный/Старт/Рост/Масштаб, all "до 5 касс").
--    growth = Рост (основной) keeps the existing {{price.growth}} token working.
DELETE FROM tariffs WHERE key='launch';
INSERT INTO tariffs (key, price_tenge, cashier_limit) VALUES
    ('trial',      0, 5),
    ('start',  10000, 5),
    ('growth', 25000, 5),
    ('scale',  60000, 5)
ON CONFLICT(key) DO UPDATE SET
    price_tenge=excluded.price_tenge, cashier_limit=excluded.cashier_limit, updated_at=datetime('now');

-- 3. Monthly payment-count limits have no schema column, so they live as bilingual
--    tokens (ns 'pay' — NOT 'price'/'limit', which map to tariffs).
INSERT INTO placeholders (token, value_ru, value_kk) VALUES
    ('pay.trial',  '3 бесплатных дня',          '3 тегін күн'),
    ('pay.start',  'до 250 платежей в месяц',   'айына 250 төлемге дейін'),
    ('pay.growth', 'до 2 000 платежей в месяц', 'айына 2 000 төлемге дейін'),
    ('pay.scale',  'безлимит платежей',         'шексіз төлемдер')
ON CONFLICT(token) DO UPDATE SET value_ru=excluded.value_ru, value_kk=excluded.value_kk;

-- 4. Topics (RU). Bodies use price tokens only; keywords steer topic/media selection.
INSERT INTO kb_topics (slug, language, title, summary, body_md, keywords, active) VALUES
('how_it_works', 'ru', 'Как работает xpayment',
 'Обзор: как xpayment подключает приём оплаты через Kaspi Pay',
 'xpayment подключает приём оплаты через Kaspi Pay за минуты. Схема: касса Kaspi Pay → виртуальная касса xpayment → xpayment API → ваш продукт (сайт, приложение, бот, CRM). Клиент платит по QR, Deeplink или ссылке на оплату. Деньги приходят сразу на ваш счёт Kaspi — мы не храним ваши средства.',
 'как работает, что такое xpayment, kaspi pay, интеграция, api, qr, deeplink, ссылка на оплату, как устроено', 1),
('tariffs', 'ru', 'Тарифы',
 'Цены и лимиты по тарифам Пробный/Старт/Рост/Масштаб',
 'xpayment — 4 тарифа для приёма оплаты через Kaspi Pay:
- Пробный — бесплатно ({{pay.trial}}), до {{limit.growth}} касс. Для первого знакомства.
- Старт — {{price.start}}/мес, {{pay.start}}, до {{limit.start}} касс. Для небольшого бизнеса.
- Рост — {{price.growth}}/мес, {{pay.growth}}, до {{limit.growth}} касс. Основной выбор для растущего бизнеса.
- Масштаб — {{price.scale}}/мес, {{pay.scale}}, до {{limit.scale}} касс. Для большого объёма.
Во всех тарифах: безлимит вебхуков, QR / Deeplink / Инвойс, полный доступ к панели.',
 'тариф, тарифы, цена, стоимость, сколько стоит, план, подписка, пробный, старт, рост, масштаб', 1),
('add_cashier', 'ru', 'Как добавить кассира',
 'Шаги добавления кассира в Kaspi Pay',
 'Как добавить кассира в Kaspi Pay (около 2 минут):
1. На главном экране Kaspi Pay откройте «Точки продаж и кассиры».
2. Выберите точку продаж, в которую добавляете кассира.
3. Добавьте кассира: ФИО, номер телефона, роль «Кассир». Нажмите «Сохранить».
4. Кассир получит SMS-приглашение и сможет войти в Kaspi Pay.
Роль «Кассир» может только принимать платежи — не переводить деньги и не менять настройки.',
 'кассир, добавить кассира, подключить кассу, точка продаж, kaspi pay, сотрудник, доступ', 1),
('waybill_payment', 'ru', 'Оплата накладных',
 'Приём оплаты накладных через статический QR и уведомления',
 'Принимайте оплату накладных через статический QR от xpayment — быстро и без лишних действий:
1. Откройте статический QR (Static QR).
2. Покупатель сканирует QR и оплачивает через Kaspi.
3. Вы видите оплату в личном кабинете xpayment.
4. Получаете уведомление в Telegram.
Надёжно и удобно для поставок и накладных.',
 'накладная, накладные, оплата накладной, статический qr, static qr, уведомления, telegram, поставка', 1),
('online_store', 'ru', 'Онлайн-оплата для интернет-магазина',
 'Приём оплаты на сайте или в приложении',
 'Подключите приём оплаты Kaspi Pay на вашем сайте или в приложении за 5 минут через xpayment. Клиент оплачивает по QR или Deeplink — простая интеграция без сложной разработки. Деньги сразу на ваш счёт Kaspi.',
 'интернет-магазин, сайт, приложение, онлайн оплата, e-commerce, deeplink, qr, оплата на сайте', 1),
('whatsapp_sales', 'ru', 'Оплата через WhatsApp',
 'Продажа и приём оплаты прямо в переписке WhatsApp',
 'Продавайте прямо в WhatsApp: отправьте клиенту ссылку на оплату в чат, клиент оплачивает через Kaspi Pay, а деньги приходят сразу на ваш счёт. Удобно для продаж в переписке — настройка за 5 минут.',
 'whatsapp, ватсап, чат, переписка, ссылка на оплату, мессенджер, продажа в чате', 1)
ON CONFLICT(slug, language) DO UPDATE SET
    title=excluded.title, summary=excluded.summary, body_md=excluded.body_md,
    keywords=excluded.keywords, active=1, updated_at=datetime('now');

-- Keep the existing security topic, just add keywords.
UPDATE kb_topics SET keywords='безопасность, безопасно ли, пароль, роль кассир, доступ, мошенничество'
    WHERE slug='security' AND keywords='';

-- 5. Replace the old phantom assets with the real git-tracked /media/kb_* files.
DELETE FROM kb_assets WHERE ref IN ('tariffs_table_ru', 'add_cashier_video_kk', 'cashier_role_ru');
INSERT INTO kb_assets (ref, topic_slug, kind, url, title, description, language, active) VALUES
('how_it_works_overview', 'how_it_works', 'image', '/media/kb_how-it-works_overview.png',
 'Как работает xpayment',
 'Полная инфографика: как xpayment подключает Kaspi Pay (виртуальная касса → API → ваш продукт, оплата по QR/ссылке). Для вопросов «как это работает / как устроено».', 'ru', 1),
('how_it_works_banner', 'how_it_works', 'image', '/media/kb_how-it-works_banner.png',
 'Как работает xpayment (баннер)',
 'Короткий баннер-схема работы xpayment + Kaspi Pay. Для краткого ответа «как работает» или знакомства.', 'ru', 1),
('tariffs_overview', 'tariffs', 'image', '/media/kb_tariffs_overview.png',
 'Тарифы — все планы',
 'Инфографика всех 4 тарифов сразу (Пробный/Старт/Рост/Масштаб) с ценами и лимитами. Для общего вопроса о тарифах/ценах.', 'ru', 1),
('tariffs_trial', 'tariffs', 'image', '/media/kb_tariffs_trial.png',
 'Тариф Пробный',
 'Карточка тарифа «Пробный»: бесплатно, 3 дня, до 5 касс. Для вопроса про бесплатный тест/пробный период.', 'ru', 1),
('tariffs_start', 'tariffs', 'image', '/media/kb_tariffs_start.png',
 'Тариф Старт',
 'Карточка тарифа «Старт»: 10 000 ₸/мес, до 250 платежей. Для небольшого бизнеса.', 'ru', 1),
('tariffs_growth', 'tariffs', 'image', '/media/kb_tariffs_growth.png',
 'Тариф Рост',
 'Карточка тарифа «Рост»: 25 000 ₸/мес, до 2 000 платежей — основной выбор. Для растущего бизнеса.', 'ru', 1),
('tariffs_scale', 'tariffs', 'image', '/media/kb_tariffs_scale.png',
 'Тариф Масштаб',
 'Карточка тарифа «Масштаб»: 60 000 ₸/мес, безлимит платежей. Для большого объёма.', 'ru', 1),
('add_cashier_guide', 'add_cashier', 'image', '/media/kb_add-cashier_guide.jpg',
 'Как добавить кассира',
 'Пошаговая инструкция (4 шага): как добавить кассира в Kaspi Pay. Для вопроса «как добавить/подключить кассира».', 'ru', 1),
('waybill_buyer_flow', 'waybill_payment', 'image', '/media/kb_waybill_buyer-flow.png',
 'Оплата накладной — путь покупателя',
 'Как покупатель оплачивает накладную через QR. Для вопроса об оплате накладных/поставок.', 'ru', 1),
('waybill_static_qr', 'waybill_payment', 'image', '/media/kb_waybill_static-qr.png',
 'Статический QR для накладных',
 'Статический QR: оплата накладных + уведомления в Telegram. Для вопроса о статическом QR / уведомлениях.', 'ru', 1),
('online_store_payment', 'online_store', 'image', '/media/kb_online-store_payment.png',
 'Онлайн-оплата для интернет-магазина',
 'Как принимать оплату на сайте/в интернет-магазине через QR и Deeplink. Для вопроса об онлайн-оплате на сайте.', 'ru', 1),
('whatsapp_sales', 'whatsapp_sales', 'image', '/media/kb_whatsapp_sales.png',
 'Оплата через WhatsApp',
 'Как продавать и принимать оплату прямо в WhatsApp по ссылке. Для вопроса о продажах/оплате в WhatsApp.', 'ru', 1)
ON CONFLICT(ref) DO UPDATE SET
    topic_slug=excluded.topic_slug, kind=excluded.kind, url=excluded.url, title=excluded.title,
    description=excluded.description, language=excluded.language, active=1, updated_at=datetime('now');
