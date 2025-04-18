import { ethers } from "ethers"
import axios from "axios"
import * as dotenv from "dotenv"

dotenv.config()

const wsEndpoint = process.env.WS_ENDPOINT!
const router = process.env.TARGET_ROUTER!.toLowerCase()
const webhookUrl = process.env.WEBHOOK_URL!
const minValue = BigInt(process.env.MIN_VALUE_WEI!)

const provider = new ethers.WebSocketProvider(wsEndpoint)
const iface = new ethers.Interface([
	"function swapExactTokensForTokens(uint256,uint256,address[],address,uint256)"
])

provider.on("pending", async (txHash: string) => {
	try {
		const tx = await provider.getTransaction(txHash)
		if (!tx || !tx.to) return

		if (
			tx.to.toLowerCase() === router &&
			tx.data.startsWith(iface.getSighash("swapExactTokensForTokens")) &&
			BigInt(tx.value.toString()) >= minValue
		) {
			const decoded = iface.decodeFunctionData(
				"swapExactTokensForTokens",
				tx.data
			)
			const tokenOut = decoded.path.at(-1)
			await axios.post(webhookUrl, {
				from: tx.from,
				tokenOut,
				value: tx.value.toString(),
				hash: tx.hash
			})
			console.log(`🚨 Pump alert: ${tx.from} → ${tokenOut} (${tx.hash})`)
		}
	} catch {
		// ignore
	}
})

console.log("⏳ Listening for pending swaps...")