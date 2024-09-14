#!/bin/bash

### Тесты для первой части задания
# Тест 1
echo "Тест 1"
curl -i -x http://127.0.0.1:8080 http://mail.ru

# Тест 2: Проверка на другой тип запроса
echo "Тест 2: Проверка на другой тип запроса"
curl -i -x http://127.0.0.1:8080 --head http://mail.ru

# Тест 3: Отправка со сторонним заголовком
echo "Тест 3: Отправка со сторонним заголовком"
curl -i -x http://127.0.0.1:8080 http://mail.ru -H "Proxy-Redirect: true"

# Тест 4: Возвращаются разные коды ответов
echo "Тест 4: Возвращаются разные коды ответов"
curl -i -x http://127.0.0.1:8080 https://mail.ru
