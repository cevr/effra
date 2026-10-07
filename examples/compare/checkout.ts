// The checkout example in TypeScript with Effect 4.0.1, for the README comparison.
// Typechecked with TypeScript 5.9 (strict, exactOptionalPropertyTypes) and run with Bun;
// the gate does not check it because the repository has no TypeScript dependency.
import { Cause, Context, Data, Effect, Layer } from "effect"

class OrderNotFound extends Data.TaggedError("OrderNotFound")<{ readonly id: string }> {}
class GatewayDown extends Data.TaggedError("GatewayDown") {}

interface Order {
  readonly id: string
  readonly total: number
}

type Payment = Data.TaggedEnum<{
  Pending: {}
  Authorized: { readonly authId: string }
  Declined: { readonly reason: string }
}>
const Payment = Data.taggedEnum<Payment>()

class Orders extends Context.Service<Orders, {
  readonly find: (id: string) => Effect.Effect<Order, OrderNotFound>
}>()("Orders") {}

class Gateway extends Context.Service<Gateway, {
  readonly authorize: (order: Order) => Effect.Effect<Payment, GatewayDown>
}>()("Gateway") {}

// Inferred: Effect<string, OrderNotFound | GatewayDown | TimeoutError, Orders | Gateway>
const checkout = (id: string) =>
  Effect.gen(function* () {
    const orders = yield* Orders
    const gateway = yield* Gateway
    const order = yield* orders.find(id)
    const payment = yield* gateway.authorize(order).pipe(Effect.timeout("500 millis"))
    return Payment.$match(payment, {
      Pending: () => "pending",
      Authorized: ({ authId }) => `paid ${authId}`,
      Declined: ({ reason }) => `declined: ${reason}`
    })
  })

const Live = Layer.mergeAll(
  Layer.succeed(Orders, {
    find: (id) =>
      id === "42" ? Effect.succeed({ id, total: 1999 }) : Effect.fail(new OrderNotFound({ id }))
  }),
  Layer.succeed(Gateway, {
    authorize: () => Effect.succeed(Payment.Authorized({ authId: "auth-7" }))
  })
)

const report = (id: string) =>
  checkout(id).pipe(
    Effect.provide(Live),
    Effect.catchTags({
      OrderNotFound: () => Effect.succeed("no such order"),
      GatewayDown: () => Effect.succeed("gateway down"),
      TimeoutError: () => Effect.succeed("gateway timed out")
    }),
    Effect.flatMap((line) => Effect.sync(() => console.log(line)))
  )

Effect.runPromise(Effect.andThen(report("42"), report("7")))

// The contract above is inferred, not written; this assertion keeps the comment honest.
const contract: (id: string) => Effect.Effect<
  string,
  OrderNotFound | GatewayDown | Cause.TimeoutError,
  Orders | Gateway
> = checkout
void contract
