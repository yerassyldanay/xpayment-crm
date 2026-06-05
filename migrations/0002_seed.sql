-- 0002_seed.sql — minimal published config so the service boots usable.
-- Edit/replace everything here through the admin UI; this is only a starting point.
-- Content mirrors the illustrative examples in docs/10-prompt-and-examples.md.

INSERT INTO assistant_config (version, status, persona, mission, guardrails, language_policy, reply_max_words, published_at)
SELECT 1, 'published',
'You are "xpayment-ассистент" — a friendly, competent sales assistant for Kazakhstani merchants. Tone: helpful, concrete, no fluff, no hard-selling. You sound like a knowledgeable colleague, not a brochure. You explain simply (most customers are non-technical business owners).',
'Help the merchant understand xpayment, pick the right tariff, and take the next step (test it, add a cashier, or talk to a human) — while quietly learning what kind of business they are.',
'- Never promise features not in the knowledge base.
- Never quote a price you weren''t given as a token; never do mental math on prices.
- Never claim to be an official Kaspi partner.
- If asked about legality/contracts/refund disputes or the customer is angry → escalate.
- Don''t push tariffs; recommend the smallest plan that fits the stated volume.
- Always offer a concrete next step (a link, an image, or "хотите, подключим за 5 минут?").',
'Reply in the customer''s language. If the latest message mixes Kazakh and Russian, reply in Russian.',
120, datetime('now')
WHERE NOT EXISTS (SELECT 1 FROM assistant_config);

INSERT OR IGNORE INTO tariffs (key, price_tenge, cashier_limit) VALUES
    ('launch',  9900, 1),
    ('growth', 19900, 5),
    ('scale',  39900, 20);

INSERT OR IGNORE INTO placeholders (token, value_ru, value_kk) VALUES
    ('support.phone', '+7 700 000 00 00', '+7 700 000 00 00');

INSERT OR IGNORE INTO kb_topics (slug, language, title, summary, body_md) VALUES
    ('tariffs', 'ru', 'Тарифы', 'Цены и лимиты касс по тарифам',
'xpayment подключает приём оплаты через Kaspi за минуты: QR, ссылка на оплату и счёт в чат. Деньги приходят сразу на ваш счёт Kaspi — мы не храним ваши средства.
Тарифы:
- Пробный — бесплатно, 3 дня.
- Launch — {{price.launch}}/мес, до {{limit.launch}} кассы. Для небольших магазинов.
- Growth — {{price.growth}}/мес, до {{limit.growth}} касс. Для растущего оборота.
- Scale — {{price.scale}}/мес, до {{limit.scale}} касс. Для большого объёма.'),
    ('security', 'ru', 'Безопасность', 'Объяснение роли «Кассир» — почему безопасно',
'Это безопасно: вы создаёте в Kaspi роль «Кассир», а не передаёте пароль. Кассир может только принимать платежи — не может переводить деньги, видеть баланс или менять настройки. Даже при компрометации доступа вывести средства нельзя.'),
    ('add_cashier', 'kk', 'Кассирді қосу', 'Виртуалды кассирді қосу қадамдары',
'Kaspi-де виртуалды кассирді қосу — 2 минут:
1. Kaspi Pay-да «Кассирлер» бөліміне кіріңіз.
2. Жаңа кассир қосып, рөлін «Кассир» етіп таңдаңыз (тек төлем қабылдау).
3. xpayment берген кодты енгізіп, SMS-тегі OTP-ны растаңыз.
Кассир тек төлем қабылдай алады — ақша аудара алмайды.');

INSERT OR IGNORE INTO kb_assets (ref, topic_slug, kind, url, title, description, language) VALUES
    ('tariffs_table_ru', 'tariffs', 'image', '/media/tariffs/pricing-ru.png', 'Тарифы (инфографика)',
     'Инфографика всех тарифов: цена + лимит касс (RU). Для вопросов о тарифах/выборе плана.', 'ru'),
    ('add_cashier_video_kk', 'add_cashier', 'screen_recording', '/media/onboarding/add-cashier-kk.mp4', 'Қосу (бейне)',
     '30-сек запись (KK): как добавить кассира и ввести OTP. Для вопроса "как подключить кассу".', 'kk'),
    ('cashier_role_ru', 'security', 'image', '/media/security/cashier-role-ru.png', 'Роль «Кассир»',
     'Что может и не может роль «Кассир» (RU). Для возражений про безопасность/пароль.', 'ru');
