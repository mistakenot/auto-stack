# Worked example: a UBIQUITOUS_LANGUAGE.md

This is a complete, realistic glossary for a fictional e-commerce project. Use it as a target
for what "done" looks like — note the tight definitions, the `_Avoid_` line on every entry,
the grouping under `##` subheadings, and the total absence of implementation detail. It is not
a template to copy verbatim; it shows the *shape* and *quality bar*.

---

# Ubiquitous Language

The canonical vocabulary for Acme Storefront. When a concept below has a canonical term, use
it everywhere — chat, code, comments, commit messages — and treat the words under `_Avoid_` as
wrong. One word per concept, shared by people, code, and agents.

## Ordering

**Order**:
A confirmed request from a Customer for one or more items at agreed prices.
_Avoid_: Purchase, transaction
_Has_: one Customer, many Line Items, many Shipments

**Cart**:
A Customer's in-progress selection of items that has not yet been confirmed as an Order.
_Avoid_: Basket, bag, pending order
_Has_: one Customer, many Line Items

**Line Item**:
A single item-and-quantity within an Order.
_Avoid_: Order row, entry, product line

## Customers

**Customer**:
A person or organization that places Orders. Identified by a CustomerId.
_Avoid_: Client, buyer, user
_Has_: many Orders, one Account

**Account**:
A Customer's authenticated login and its settings. A Customer may exist without an Account
(guest checkout); an Account always belongs to one Customer.
_Avoid_: Profile, user, login
_Has_: one Customer

## Fulfilment

**Shipment**:
A package of one or more Line Items dispatched to a Customer. One Order may produce several
Shipments.
_Avoid_: Delivery, parcel, dispatch
_Has_: one Order, many Line Items

**Cancellation**:
Voiding an Order, or specific Line Items, before they are dispatched. After dispatch the
equivalent action is a Return.
_Avoid_: Refund (that's the money movement), void, abort

**Return**:
A Customer sending dispatched items back. Distinct from a Cancellation (pre-dispatch) and from
a Refund (the money movement that may follow either).
_Avoid_: Send-back, RMA (that's the tracking number, not the concept)

## Billing

**Invoice**:
A request for payment issued to a Customer for an Order.
_Avoid_: Bill, statement, payment request

**Refund**:
Returning money to a Customer. May follow a Cancellation or a Return, but is its own concept —
the money movement, not the goods movement.
_Avoid_: Reimbursement, payback, credit
