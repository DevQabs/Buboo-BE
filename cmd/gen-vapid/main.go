// gen-vapid 는 웹 푸시 VAPID 키 한 쌍을 Render 환경변수 줄로 찍는다. 한 번만 쓴다(push 설계서 6절).
// 실행: go run ./cmd/gen-vapid | pbcopy   → Render 에 붙여 넣고 VAPID_SUBJECT(FE https 주소)를 더한다.
package main

import (
	"fmt"
	"log"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func main() {
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", pub, priv)
}
