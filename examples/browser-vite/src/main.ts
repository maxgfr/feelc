import { createEngine } from "@feelc/engine";

const SOURCE = `model "promo" {}
input cart_total : number >= 0
input is_member  : boolean
decision discount_pct : number {
  needs: cart_total, is_member
  hit: collect max
  >= 50  | -    => 5
  >= 100 | -    => 10
  -      | true => 8
}`;

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

async function main() {
  const feelc = await createEngine(); // loads feelc.wasm (resolved by Vite as an asset)
  const model = feelc.compile(SOURCE); // compile once; evaluate on every input change

  const cart = $<HTMLInputElement>("cart");
  const member = $<HTMLInputElement>("member");
  const out = $<HTMLOutputElement>("out");

  const evaluate = () => {
    const { output } = model.evaluate("discount_pct", {
      cart_total: Number(cart.value),
      is_member: member.checked,
    });
    out.textContent = output === null ? "0" : String(output);
  };

  cart.addEventListener("input", evaluate);
  member.addEventListener("change", evaluate);

  $("status").hidden = true;
  $("form").hidden = false;
  evaluate();
}

main().catch((err) => {
  $("status").textContent = `failed: ${err?.message ?? err}`;
});
