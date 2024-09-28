.PHONY: gen-ca
gen-ca:
	# генерация приватного ключа CA
	openssl genpkey -algorithm RSA -out ca.key -pkeyopt rsa_keygen_bits:2048
	# генерация самоподписанного сертификата CA
	openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 -out ca.crt -subj "/C=US/ST=State/L=City/O=Organization/OU=OrgUnit/CN=RootCA"

