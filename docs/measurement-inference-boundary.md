# Measurement and Inference Boundary

## Unpriced Rungs and Holes

When upstream liquidity fails or a rung cannot be priced at a specific trade size, Wayfare explicitly marks that rung as unpriced. No rate or interpolated value is ever synthesized or assigned to fill the gap. The curve preserves holes transparently as part of the execution economics discipline.
